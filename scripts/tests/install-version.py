"""Check streamed installer behavior with fake services; never touch a cluster."""
import io
import json
import os
from pathlib import Path
import subprocess
import shutil
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
                        REAL_NODE=shutil.which("node"),
                        K8S_CLUSTER="installer-test", K8S_PORT="4321")
        self.release = self.root / "release.json"
        self.release.write_text(json.dumps({"tag_name": "v0.1.0", "draft": False, "prerelease": False}))
        with tarfile.open(self.root / "source.tgz", "w:gz") as archive:
            for version in ("0.1.0", "0.1.0-rc.1"):
                folder = tarfile.TarInfo(f"runloom-{version}/scripts")
                folder.type = tarfile.DIRTYPE
                folder.mode = 0o755
                archive.addfile(folder)
                payload = b"// Fixture only. Node is mocked.\n"
                info = tarfile.TarInfo(f"runloom-{version}/scripts/local-k8s.mjs")
                info.size = len(payload)
                archive.addfile(info, io.BytesIO(payload))
        for tool in ("docker", "kind", "kubectl", "helm", "node", "curl"):
            path = self.bin / tool
            path.write_text('''#!/bin/sh
set -eu
name=${0##*/}
printf '%s %s\n' "$name" "$*" >> "$FIXTURES/calls"
case "$name" in
  curl) case "$*" in
          *api.github.com/repos/itsneel/runloom/releases/latest*)
            [ "${FAIL_LOOKUP:-0}" = 0 ] || exit 22
            cat "$FIXTURES/release.json"
            exit 0 ;;
        esac
        [ "${FAIL_DOWNLOAD:-0}" = 0 ] || exit 22
        cat "$FIXTURES/source.tgz" ;;
  helm) mkdir -p charts/runloom ;;
  node) if [ "$1" = -e ]; then exec "$REAL_NODE" "$@"; fi
        test -f scripts/local-k8s.mjs
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
        self.assertIn("[VERSION] [--no-forward]", result.stdout)
        self.assertEqual(self.calls(), "")

    def test_invalid_arguments_do_not_install(self):
        for args in (("latest",), ("v",), ("../oops",), ("0.1.0", "--unknown"),
                     ("--no-forward", "0.1.0"), ("0.1.0", "--no-forward", "extra")):
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
        self.assertNotIn("api.github.com", self.calls())

    def test_omitted_version_resolves_once_and_forwards(self):
        result = self.run_installer()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.calls().count("https://api.github.com/repos/itsneel/runloom/releases/latest"), 1)
        self.assertIn("Selected Runloom 0.1.0", result.stdout)
        self.assertIn("/refs/tags/v0.1.0", self.calls())
        self.assertIn("--version 0.1.0 --untar", self.calls())
        self.assertIn("kubectl ", self.calls())

    def test_latest_without_forwarding_and_empty_make_version(self):
        for args in (("--no-forward",), ("", "--no-forward")):
            with self.subTest(args=args):
                result = self.run_installer(*args)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertNotIn("kubectl ", self.calls())

    def test_explicit_prerelease_skips_latest_lookup(self):
        result = self.run_installer("0.1.0-rc.1", "--no-forward", FAIL_LOOKUP="1")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("--version 0.1.0-rc.1 --untar", self.calls())
        self.assertNotIn("api.github.com", self.calls())

    def test_lookup_failure_stops_before_downloading_or_installing(self):
        result = self.run_installer(FAIL_LOOKUP="1")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("supply an explicit VERSION", result.stderr)
        self.assertNotIn("codeload.github.com", self.calls())
        self.assertNotIn("helm ", self.calls())
        self.assertNotIn("node --input-type", self.calls())

    def test_invalid_latest_metadata_is_rejected(self):
        for metadata in ('not JSON', '{}', 'null',
                         '{"tag_name":"../oops","draft":false,"prerelease":false}',
                         '{"tag_name":"v0.1.0","draft":true,"prerelease":false}',
                         '{"tag_name":"v0.1.0","draft":false,"prerelease":true}',
                         '{"tag_name":"v0.1.0-rc.1","draft":false,"prerelease":false}'):
            with self.subTest(metadata=metadata):
                self.release.write_text(metadata)
                result = self.run_installer()
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("supply an explicit VERSION", result.stderr)
                self.assertNotIn("codeload.github.com", self.calls())
                self.assertNotIn("helm ", self.calls())
                self.assertNotIn("node --input-type", self.calls())

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
