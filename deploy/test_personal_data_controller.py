import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from personal_data_controller import execute, validate_request


class PersonalDataDeploymentTests(unittest.TestCase):
    def test_rejects_untrusted_requests(self):
        for request in ({}, {'action': 'shell', 'tag': 'v1.0.0'},
                        {'action': 'deploy', 'tag': '../../root'},
                        {'action': 'deploy', 'tag': 'v1.0.0', 'token': 'test', 'registry_user': 'a;id'}):
            with self.assertRaises(ValueError):
                validate_request(request)

    def test_independent_release_and_rollback(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / '.env').write_text('CULLY_DB_NAME=cully\n')
            calls = []
            def runner(args, **kwargs):
                calls.append((args, kwargs))
            request = dict(action='deploy', tag='v1.0.0', token='test-token', registry_user='tester')
            execute(request, root, runner)
            execute({'action': 'rollback', 'tag': 'v1.0.0'}, root, runner)
            self.assertIsNone(json.loads((root / 'state.json').read_text())['current'])
            execute(request, root, runner)
            request['tag'] = 'v1.0.1'
            execute(request, root, runner)
            self.assertEqual(json.loads((root / 'state.json').read_text()),
                             {'current': 'v1.0.1', 'previous': 'v1.0.0'})
            self.assertNotIn('test-token', str([args for args, _ in calls]))
            self.assertTrue(any('cully-personal-data' in args for args, _ in calls))
            self.assertTrue(any('ghcr.io/tavric-labs/cully-mem0:v1.0.0' in args for args, _ in calls))
            self.assertTrue(any('migrate' in args for args, _ in calls))
            execute({'action': 'rollback', 'tag': 'v1.0.1'}, root, runner)
            self.assertEqual(json.loads((root / 'state.json').read_text())['current'], 'v1.0.0')

    def test_unhealthy_release_restores_previous(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / '.env').write_text('CULLY_DB_NAME=cully\n')
            (root / 'state.json').write_text(json.dumps({'current': 'v1.0.0', 'previous': None}))
            activated = []
            def runner(args, **kwargs):
                if 'compose' in args:
                    tag = kwargs['env']['CULLY_DATA_TAG']
                    activated.append(tag)
                    if tag == 'v1.0.1' and 'cully-data-api' in args:
                        raise subprocess.CalledProcessError(1, args)
            with self.assertRaises(subprocess.CalledProcessError):
                execute(dict(action='deploy', tag='v1.0.1', token='test', registry_user='tester'), root, runner)
            self.assertEqual(activated, ['v1.0.1', 'v1.0.1', 'v1.0.1', 'v1.0.0', 'v1.0.0', 'v1.0.0'])
            self.assertEqual(json.loads((root / 'state.json').read_text())['current'], 'v1.0.0')


if __name__ == '__main__':
    unittest.main()
