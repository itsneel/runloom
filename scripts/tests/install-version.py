"""Check streamed installer behavior with fake services; never touch a cluster."""
import io
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest


SCRIPT = (Path(__file__).resolve().parents[1] / "install-version.sh").read_text()


class InstallerTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.bin = self.root / "bin"
        self.bin.mkdir()
        (self.root / "tmp").mkdir()
        self.env = dict(os.environ, PATH=str(self.bin) + os.pathsep + os.environ["PATH"],
                        TMPDIR=str(self.root / "tmp"), FIXTURES=str(self.root),
                        K8S_CLUSTER="installer-test", K8S_PORT="4321")
        with tarfile.open(self.root / "source.tgz", "w:gz") as archive:
            folder = tarfile.TarInfo("runloom-0.1.0/scripts")
            folder.type = tarfile.DIRTYPE
            folder.mode = 0o755
            archive.addfile(folder)
            payload = b"// Fixture only. Node is mocked.\n"
            info = tarfile.TarInfo("runloom-0.1.0/scripts/local-k8s.mjs")
            info.size = len(payload)
            archive.addfile(info, io.BytesIO(payload))
        for tool in ("docker", "kind", "kubectl", "helm", "node", "curl"):
            path = self.bin / tool
            path.write_text('''#!/bin/sh
set -eu
name=${0##*/}
printf '%s %s\n' "$name" "$*" >> "$FIXTURES/calls"
case "$name" in
  curl) [ "${FAIL_DOWNLOAD:-0}" = 0 ] || exit 22
        cat "$FIXTURES/source.tgz" ;;
  helm) mkdir -p charts/runloom ;;
  node) test -f scripts/local-k8s.mjs
        cat > "$FIXTURES/node-input"
        exit "${FAIL_NODE:-0}" ;;
esac
''')
            path.chmod(0o755)

    def run_installer(self, *args, source=SCRIPT, **env):
        result = subprocess.run(["bash", "-s", "--", *args], input=source,
                                cwd=self.root, env=dict(self.env, **env), text=True,
                                capture_output=True, timeout=10)
        self.assertEqual(list((self.root / "tmp").iterdir()), [], "Temporary files leaked")
        return result

    def calls(self):
        path = self.root / "calls"
        return path.read_text() if path.exists() else ""

    def test_help_does_not_install(self):
        result = self.run_installer("--help")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("VERSION [--no-forward]", result.stdout)
        self.assertEqual(self.calls(), "")

    def test_invalid_arguments_do_not_install(self):
        for args in ((), ("latest",), ("../oops",), ("0.1.0", "--unknown")):
            with self.subTest(args=args):
                self.assertEqual(self.run_installer(*args).returncode, 2)
                self.assertEqual(self.calls(), "")

    def test_streamed_install_selects_version_and_skips_forwarding(self):
        result = self.run_installer("v0.1.0", "--no-forward")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("/refs/tags/v0.1.0", self.calls())
        self.assertIn("helm pull oci://ghcr.io/itsneel/charts/runloom --version 0.1.0", self.calls())
        self.assertIn("await ensureLocalAdmin", (self.root / "node-input").read_text())
        self.assertNotIn("kubectl ", self.calls())

    def test_forwarding_preserves_overrides(self):
        result = self.run_installer("0.1.0")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("kubectl --context kind-installer-test -n runloom port-forward "
                      "--address 127.0.0.1 svc/runloom-ui 4321:80", self.calls())

    def test_failures_stop_before_next_stage(self):
        result = self.run_installer("0.1.0", FAIL_DOWNLOAD="1")
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn("helm ", self.calls())
        self.assertNotIn("node ", self.calls())
        result = self.run_installer("0.1.0", FAIL_NODE="7")
        self.assertEqual(result.returncode, 7, result.stderr)
        self.assertNotIn("kubectl ", self.calls())

    def test_truncated_download_never_starts_installation(self):
        result = self.run_installer("0.1.0", source=SCRIPT[:SCRIPT.index("  node --input")])
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.calls(), "")


if __name__ == "__main__":
    unittest.main()
