"""Exercise the installer without network access or real agent configuration."""
import hashlib
import io
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest


INSTALLER = Path(__file__).resolve().parents[1] / "install.sh"


class InstallerTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.bin = self.root / "bin"
        self.bin.mkdir()
        self.cli = self.root / "cully"
        self.cli.write_text('''#!/bin/sh
case "$1" in
  version) echo 'cully v-test' ;;
  _internal) printf '%s\\n' "$*" > "$TEST_ROOT/daemon-call" ;;
  setup) printf '%s\\n' "$*" > "$TEST_ROOT/setup"; printf '%s\\n' "$PATH" > "$TEST_ROOT/setup-path" ;;
esac
''')
        self.cli.chmod(0o755)
        with tarfile.open(self.root / "archive.tar.gz", "w:gz") as archive:
            content = self.cli.read_bytes()
            entry = tarfile.TarInfo("cully")
            entry.size = len(content)
            entry.mode = 0o755
            archive.addfile(entry, io.BytesIO(content))
        digest = hashlib.sha256((self.root / "archive.tar.gz").read_bytes()).hexdigest()
        (self.root / "checksums.txt").write_text(f"{digest}  cully_linux_amd64.tar.gz\n")
        self.stub("uname", 'case "$1" in -s) echo Linux ;; -m) echo x86_64 ;; esac')
        self.stub("curl", '''
printf '%s\\n' "$*" >> "$TEST_ROOT/curl-calls"
[ "${DOWNLOAD_FAIL:-0}" = 0 ] || exit 28
source_file=archive.tar.gz
while [ "$#" -gt 0 ]; do
  case "$1" in
    */checksums.txt) source_file=checksums.txt ;;
    -o) shift; destination="$1" ;;
  esac
  shift
done
cp "$TEST_ROOT/$source_file" "$destination"
''')
        self.stub("go", '''
printf '%s\\n' "$*" > "$TEST_ROOT/go-call"
cp "$TEST_ROOT/cully" "$GOBIN/cully"
''')
        self.env = dict(os.environ, TEST_ROOT=str(self.root), HOME=str(self.root), SHELL="/bin/zsh",
                        CLAUDE_CONFIG_DIR=str(self.root / "claude"),
                        CODEX_HOME=str(self.root / ".codex"),
                        CURSOR_CONFIG_DIR=str(self.root / ".cursor"),
                        PATH=str(self.bin) + os.pathsep + os.environ["PATH"])

    def stub(self, name, body):
        path = self.bin / name
        path.write_text("#!/bin/sh\nset -eu\n" + body + "\n")
        path.chmod(0o755)

    def run_installer(self, *args):
        return subprocess.run(["sh", str(INSTALLER), *args], env=self.env,
                              capture_output=True, text=True, timeout=10)

    def test_binary_install_forwards_agent_and_mcp_options(self):
        result = self.run_installer("--agent", "codex", "--mcp-url",
                                    "https://example.com/mcp", "--oauth")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual((self.root / "setup").read_text().strip(),
                         "setup --mcp-url https://example.com/mcp --agent codex --oauth")
        self.assertFalse((self.root / "go-call").exists())
        calls = (self.root / "curl-calls").read_text()
        self.assertIn("--progress-bar --connect-timeout 10 --max-time 120", calls)
        self.assertIn("--speed-limit 1024 --speed-time 30", calls)
        self.assertIn("--max-time 30", calls)

    def test_failed_download_does_not_compile_or_install(self):
        self.env["DOWNLOAD_FAIL"] = "1"
        result = self.run_installer()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("--from-source", result.stderr)
        self.assertFalse((self.root / "go-call").exists())
        self.assertFalse((self.root / ".local" / "bin" / "cully").exists())

    def test_explicit_source_install_uses_selected_ref(self):
        for version, ref in [("latest", "main"), ("v0.3.0", "v0.3.0")]:
            with self.subTest(version=version):
                self.env["CULLY_VERSION"] = version
                result = self.run_installer("--from-source", "--agent", "cursor")
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertIn("several minutes", result.stdout)
                self.assertEqual((self.root / "go-call").read_text().strip(),
                                 f"install -v github.com/mcp-runtime/cully/cmd/cully@{ref}")
                self.assertFalse((self.root / "curl-calls").exists())

    def old_binary(self, path, version="0.3.0"):
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(f"#!/bin/sh\necho 'cully {version}'\n")
        path.chmod(0o755)
        return path

    def test_bare_install_upgrades_selected_old_copy_and_explains_shell(self):
        old = self.old_binary(self.root / ".claude" / "bin" / "cully")
        self.env["PATH"] = str(old.parent) + os.pathsep + self.env["PATH"]
        result = self.run_installer()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(old.read_bytes(), self.cli.read_bytes())
        installed = self.root / ".local" / "bin" / "cully"
        self.assertEqual(installed.read_bytes(), self.cli.read_bytes())
        self.assertIn(f"Current PATH selects: {old} (cully 0.3.0)", result.stdout)
        self.assertTrue(old.is_symlink())
        self.assertEqual(old.readlink(), installed)
        self.assertIn(f"{old} -> {installed}", result.stdout)
        self.assertIn("cannot change the PATH or command cache", result.stdout)
        self.assertIn('export PATH="$HOME/.local/bin:$PATH"; hash -r', result.stdout)
        self.assertIn("command -v cully; cully version", result.stdout)
        self.assertIn(f'"{installed}" setup --all', result.stdout)
        self.assertIn(f'"{installed}" setup --mcp-url URL', result.stdout)
        self.assertNotIn("setup --agent codex --all", result.stdout)
        self.assertFalse((self.root / "setup").exists())
        self.assertEqual((self.root / "daemon-call").read_text().strip(), "_internal stop-daemon")

    def test_agent_choice_uses_same_cli_and_refreshes_historical_copies(self):
        copies = [self.old_binary(self.root / name / "bin" / "cully")
                  for name in [".claude", ".codex", ".cursor", "claude"]]
        for agent in ["codex", "claude", "cursor", "all"]:
            with self.subTest(agent=agent):
                result = self.run_installer("--agent", agent)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertTrue((self.root / ".local" / "bin" / "cully").exists())
                installed = self.root / ".local" / "bin" / "cully"
                self.assertIn(f'"{installed}" setup --agent {agent} --all', result.stdout)
                for copy in copies:
                    self.assertTrue(copy.is_symlink())
                    self.assertEqual(copy.readlink(), self.root / ".local" / "bin" / "cully")
                    self.assertEqual(copy.read_bytes(), self.cli.read_bytes())
        profile = (self.root / ".zshrc").read_text()
        self.assertEqual(profile.count('export PATH="$HOME/.local/bin:$PATH"'), 1)

    def test_selected_custom_home_installation_is_upgraded(self):
        old = self.old_binary(self.bin / "cully")
        result = self.run_installer()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(old.read_bytes(), self.cli.read_bytes())
        self.assertTrue(old.is_symlink())
        self.assertEqual(old.readlink(), self.root / ".local" / "bin" / "cully")
        self.assertIn("Agent symlink", result.stdout)

    def test_repoints_cully_symlinks_and_preserves_custom_wrappers(self):
        target = self.old_binary(self.root / "linked-cully")
        link = self.root / ".claude" / "bin" / "cully"
        link.parent.mkdir(parents=True)
        link.symlink_to(target)
        wrapper = self.root / ".codex" / "bin" / "cully"
        wrapper.parent.mkdir(parents=True)
        wrapper.write_text("#!/bin/sh\necho 'custom wrapper'\n")
        wrapper.chmod(0o755)
        wrapper_contents = wrapper.read_bytes()
        target_contents = target.read_bytes()
        result = self.run_installer()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue(link.is_symlink())
        self.assertEqual(target.read_bytes(), target_contents)
        self.assertEqual(wrapper.read_bytes(), wrapper_contents)
        self.assertEqual(link.readlink(), self.root / ".local" / "bin" / "cully")
        self.assertIn(f"Agent symlink: {link}", result.stdout)
        self.assertIn(f"Preserved custom command: {wrapper}", result.stdout)

    def test_failed_download_leaves_old_binary_unchanged(self):
        old = self.old_binary(self.bin / "cully")
        contents = old.read_bytes()
        self.env["DOWNLOAD_FAIL"] = "1"
        result = self.run_installer()
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(old.read_bytes(), contents)
        self.assertFalse((self.root / ".zshrc").exists())

    def test_pinned_version_is_explicit_in_download_url(self):
        self.env["CULLY_VERSION"] = "v0.10.1"
        result = self.run_installer()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("releases/download/v0.10.1/", (self.root / "curl-calls").read_text())

    def test_canonical_symlink_becomes_the_only_installed_binary(self):
        target = self.old_binary(self.root / "linked-cully")
        destination = self.root / ".local" / "bin" / "cully"
        destination.parent.mkdir(parents=True)
        destination.symlink_to(target)
        contents = target.read_bytes()
        result = self.run_installer()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse(destination.is_symlink())
        self.assertEqual(destination.read_bytes(), self.cli.read_bytes())
        self.assertEqual(target.read_bytes(), contents)

    def test_fresh_agent_install_creates_link_to_single_binary(self):
        result = self.run_installer("--agent", "codex")
        self.assertEqual(result.returncode, 0, result.stderr)
        binary = self.root / ".local" / "bin" / "cully"
        link = self.root / ".codex" / "bin" / "cully"
        self.assertFalse(binary.is_symlink())
        self.assertTrue(link.is_symlink())
        self.assertEqual(link.readlink(), binary)
        self.assertFalse((self.root / ".cursor").exists())
        self.assertFalse((self.root / "claude").exists())

    def test_reinstall_keeps_agent_link_and_upgrades_its_target(self):
        result = self.run_installer("--agent", "codex")
        self.assertEqual(result.returncode, 0, result.stderr)
        binary = self.root / ".local" / "bin" / "cully"
        link = self.root / ".codex" / "bin" / "cully"
        inode = link.lstat().st_ino
        binary.write_text("#!/bin/sh\necho 'cully 0.3.0'\n")
        result = self.run_installer("--agent", "codex")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(link.lstat().st_ino, inode)
        self.assertEqual(link.read_bytes(), self.cli.read_bytes())
        self.assertIn("Agent link already configured", result.stdout)

    def test_repairs_dangling_agent_link(self):
        link = self.root / ".codex" / "bin" / "cully"
        link.parent.mkdir(parents=True)
        link.symlink_to(self.root / "removed-binary")
        result = self.run_installer("--agent", "codex")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(link.readlink(), self.root / ".local" / "bin" / "cully")
        self.assertEqual(link.read_bytes(), self.cli.read_bytes())

    def test_remote_setup_without_agent_lets_setup_detect(self):
        result = self.run_installer("--mcp-url", "https://example.com/mcp")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual((self.root / "setup").read_text().strip(),
                         "setup --mcp-url https://example.com/mcp")

    def test_preserves_shell_settings_and_prioritizes_canonical_path(self):
        profile = self.root / ".zshrc"
        profile.write_text("# my own settings\n")
        old = self.old_binary(self.root / ".claude" / "bin" / "cully")
        self.env["PATH"] = str(old.parent) + os.pathsep + self.env["PATH"]
        result = self.run_installer("--agent", "codex", "--mcp-url", "https://example.com/mcp")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue(profile.read_text().startswith("# my own settings\n"))
        self.assertEqual((self.root / "setup-path").read_text().split(os.pathsep)[0], str(self.root / ".local" / "bin"))

    def test_checksum_mismatch_aborts_install(self):
        old = self.old_binary(self.bin / "cully")
        contents = old.read_bytes()
        (self.root / "checksums.txt").write_text("bad  cully_linux_amd64.tar.gz\n")
        result = self.run_installer()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("checksum mismatch", result.stderr)
        self.assertEqual(old.read_bytes(), contents)
        self.assertFalse((self.root / ".local" / "bin" / "cully").exists())


if __name__ == "__main__":
    unittest.main()
