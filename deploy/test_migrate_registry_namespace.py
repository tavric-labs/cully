import os
import stat
import subprocess
import tarfile
import tempfile
import unittest
from pathlib import Path

DEPLOY = Path(__file__).resolve().parent
SCRIPT = DEPLOY / 'migrate-registry-namespace.sh'
OLD = 'ghcr.io/mcp-runtime/'
NEW = 'ghcr.io/tavric-labs/'


class MigrateRegistryNamespaceTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        root = Path(self.tmp.name)
        self.opt, self.personal = root / 'opt', root / 'personal'
        self.libexec, self.backup, self.bin = root / 'libexec', root / 'backup', root / 'bin'
        for d in (self.opt, self.personal, self.libexec, self.backup, self.bin):
            d.mkdir()
        self.log = root / 'docker.log'
        docker = self.bin / 'docker'
        docker.write_text(
            '#!/bin/sh\n'
            'echo "$@" >> "%s"\n'
            'if [ "$1" = image ]; then\n'
            '  echo %scully-web:abc123\n  echo %scully-docs:abc123\n  echo unrelated/image:1\n'
            'fi\n' % (self.log, OLD, OLD))
        docker.chmod(docker.stat().st_mode | stat.S_IEXEC)
        # An installed VM that still uses the old namespace.
        (self.libexec / 'cully-web-deploy').write_text("'image': '%scully-web:'\n" % OLD)
        (self.opt / 'compose.yaml').write_text('image: %scully-web:${R}\n' % OLD)
        (self.opt / 'compose.docs.yaml').write_text('image: %scully-docs:${R}\n' % OLD)
        (self.libexec / 'cully-personal-data-deploy').write_text("pull %scully-data:x\n" % OLD)
        (self.personal / 'compose.yaml').write_text('image: %scully-data:${T}\n' % OLD)
        (self.opt / 'state.json').write_text('{"current": "abc123", "previous": null}')

    def tearDown(self):
        self.tmp.cleanup()

    def run_script(self, *args):
        env = dict(os.environ, CULLY_MIGRATE_TEST='1', CULLY_OPT_DIR=str(self.opt),
                   CULLY_PERSONAL_DIR=str(self.personal), CULLY_LIBEXEC_DIR=str(self.libexec),
                   CULLY_BACKUP_DIR=str(self.backup), PATH='%s:%s' % (self.bin, os.environ['PATH']))
        return subprocess.run(['sh', str(SCRIPT), *args], env=env, capture_output=True, text=True)

    def test_dry_run_reports_and_changes_nothing(self):
        before = (self.opt / 'compose.yaml').read_text()
        result = self.run_script('--dry-run')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('Dry run', result.stdout)
        self.assertIn('cully-web:abc123', result.stdout)
        self.assertEqual((self.opt / 'compose.yaml').read_text(), before)
        self.assertEqual(list(self.backup.iterdir()), [])
        self.assertNotIn('tag', self.log.read_text().split())

    def test_migrates_snapshots_retags_and_keeps_state(self):
        result = self.run_script()
        self.assertEqual(result.returncode, 0, result.stderr)
        for path in (self.libexec / 'cully-web-deploy', self.opt / 'compose.yaml',
                     self.opt / 'compose.docs.yaml', self.personal / 'compose.yaml',
                     self.libexec / 'cully-personal-data-deploy'):
            text = path.read_text()
            self.assertIn(NEW, text, path)
            self.assertNotIn(OLD, text, path)
        self.assertEqual((self.opt / 'state.json').read_text(), '{"current": "abc123", "previous": null}')
        snapshots = list(self.backup.iterdir())
        self.assertEqual(len(snapshots), 1)
        self.assertEqual(stat.S_IMODE(snapshots[0].stat().st_mode), 0o600)
        with tarfile.open(snapshots[0]) as tar:
            members = ' '.join(tar.getnames())
        self.assertIn('compose.yaml', members)
        self.assertIn('state.json', members)
        log = self.log.read_text()
        self.assertIn('tag %scully-web:abc123 %scully-web:abc123' % (OLD, NEW), log)
        self.assertIn('tag %scully-docs:abc123 %scully-docs:abc123' % (OLD, NEW), log)
        self.assertNotIn('unrelated', log.replace('unrelated/image:1', ''))
        self.assertEqual(oct(stat.S_IMODE((self.libexec / 'cully-web-deploy').stat().st_mode)), '0o755')

    def test_second_run_is_a_noop(self):
        self.assertEqual(self.run_script().returncode, 0)
        before = list(self.backup.iterdir())
        again = self.run_script()
        self.assertEqual(again.returncode, 0, again.stderr)
        self.assertIn('already migrated', again.stdout)
        self.assertEqual(list(self.backup.iterdir()), before)

    def test_refuses_a_stale_checkout_and_bad_arguments(self):
        self.assertNotEqual(self.run_script('--wrong').returncode, 0)
        for source in ('website_controller.py', 'compose.website.yaml', 'compose.docs.yaml'):
            self.assertIn(NEW, (DEPLOY / source).read_text())
            self.assertNotIn(OLD, (DEPLOY / source).read_text())


if __name__ == '__main__':
    unittest.main()
