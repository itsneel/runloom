"""Run release commands against local fake registries and GitHub; no publication."""
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
DIGEST = "sha256:" + "a" * 64
OLD = "sha256:" + "b" * 64
IMAGES = ["runloom-auth", "runloom-docs"]
STUB = r'''
import json, os, shutil, subprocess, sys
from pathlib import Path
root = Path(os.environ["FIXTURE"])
args = sys.argv[1:]
name = Path(sys.argv[0]).name
state = json.loads((root / "state.json").read_text())
with (root / "calls.jsonl").open("a") as log:
    log.write(json.dumps([name, *args]) + "\n")
def save():
    (root / "state.json").write_text(json.dumps(state))
if name == "yq":
    print("\n".join(state["images"]))
elif name == "gh":
    if args[0] == "api":
        if os.environ.get("FAIL_API"): sys.exit(1)
        if args[1].endswith("/releases/latest"):
            print(state.get("latest", ""))
        else:
            assert "--paginate" in args
            result = subprocess.run(["jq", "-r", args[args.index("--jq") + 1]],
                input=json.dumps(state["releases"]), text=True, capture_output=True, check=True)
            print(result.stdout, end="")
    elif args[:2] == ["release", "download"]:
        destination = Path(args[args.index("--dir") + 1])
        shutil.copy(root / "image-digests.tsv", destination / "image-digests.tsv")
    elif args[:2] == ["release", "edit"] and "--latest" in args:
        state["latest"] = args[2]; save()
    elif args[:2] not in (["release", "edit"], ["release", "create"], ["release", "upload"]):
        raise RuntimeError(args)
elif name == "docker":
    assert args[:2] == ["buildx", "imagetools"]
    if args[2] == "inspect":
        print("Digest:", state["tags"][args[-1]])
    elif args[2] == "create":
        target = args[args.index("--tag") + 1]
        if os.environ.get("FAIL_IMAGE") in (target,): sys.exit(1)
        state["tags"][target] = args[-1].split("@", 1)[1]; save()
    else: raise RuntimeError(args)
elif name == "helm":
    if args[0] == "pull":
        chart = args[1].rsplit("/", 1)[1]
        version = args[args.index("--version") + 1]
        destination = Path(args[args.index("--destination") + 1])
        shutil.copy(root / "release" / (chart + "-" + version + ".tgz"), destination)
    else: assert args[0] == "push"
else: raise RuntimeError(name)
'''


def release(tag, **kwargs):
    return dict(tag_name=tag, draft=False, prerelease=False, **kwargs)


class LatestTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.version = "0.10.0"
        self.state = dict(images=IMAGES, releases=[release("v0.9.0"), release("v0.10.0")],
                          latest="v0.9.0", tags={})
        for image in IMAGES:
            self.state["tags"][f"ghcr.io/test/{image}:0.10.0"] = DIGEST
            self.state["tags"][f"ghcr.io/test/{image}:latest"] = OLD
        for tool in ("gh", "docker", "yq", "helm"):
            stub = self.root / tool
            stub.write_text("#!" + sys.executable + "\n" + STUB)
            stub.chmod(0o755)
        (self.root / "image-digests.tsv").write_text("".join(f"{i}\t{DIGEST}\n" for i in IMAGES))

    def run_release(self, command="latest", **env):
        (self.root / "state.json").write_text(json.dumps(self.state))
        result = subprocess.run(["bash", str(ROOT / "scripts/release.sh"), command], cwd=ROOT,
            env=dict(os.environ, PATH=str(self.root) + os.pathsep + os.environ["PATH"],
                     FIXTURE=str(self.root), RELEASE_DIR=str(self.root / "release"),
                     VERSION=self.version, REGISTRY="ghcr.io/test", GITHUB_REPOSITORY="test/runloom",
                     PAGES_URL="https://example.test/", **env), capture_output=True, text=True, timeout=20)
        self.state = json.loads((self.root / "state.json").read_text())
        return result

    def calls(self):
        path = self.root / "calls.jsonl"
        return [json.loads(line) for line in path.read_text().splitlines()] if path.exists() else []

    def assert_no_mutation(self):
        self.assertFalse(any(c[:4] == ["docker", "buildx", "imagetools", "create"] or
                             c[:3] == ["gh", "release", "edit"] for c in self.calls()))

    def test_stable_replaces_existing_latest_and_is_repeatable(self):
        for _ in range(2):
            result = self.run_release()
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(self.state["latest"], "v0.10.0")
            for image in IMAGES:
                self.assertEqual(self.state["tags"][f"ghcr.io/test/{image}:latest"], DIGEST)
        updates = [c for c in self.calls() if c[:4] == ["docker", "buildx", "imagetools", "create"]]
        self.assertTrue(all(c[-1].endswith("@" + DIGEST) for c in updates))
        self.assertTrue(all(c[-2].endswith(":latest") for c in updates))

    def test_prerelease_never_changes_latest(self):
        self.version = "1.0.0-rc.1"
        self.assertEqual(self.run_release().returncode, 0)
        self.assertEqual(self.calls(), [])

    def test_older_release_cannot_move_latest_backwards(self):
        self.version = "0.9.0"
        self.assertEqual(self.run_release().returncode, 0)
        self.assert_no_mutation()

    def test_drafts_prereleases_and_non_semver_tags_are_ignored(self):
        self.state["releases"] += [dict(tag_name="v9.0.0", draft=True, prerelease=False),
            dict(tag_name="v8.0.0", draft=False, prerelease=True), release("latest")]
        result = self.run_release()
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_unpublished_candidate_is_rejected(self):
        self.state["releases"] = [release("v0.9.0")]
        self.assertNotEqual(self.run_release().returncode, 0)
        self.assert_no_mutation()

    def test_api_failure_is_closed(self):
        self.assertNotEqual(self.run_release(FAIL_API="1").returncode, 0)
        self.assert_no_mutation()

    def test_inventory_is_checked_before_any_tag_moves(self):
        for inventory in (f"runloom-auth\t{DIGEST}\n", "".join(f"{i}\tnot-a-digest\n" for i in IMAGES)):
            (self.root / "image-digests.tsv").write_text(inventory)
            self.assertNotEqual(self.run_release().returncode, 0)
            self.assert_no_mutation()

    def test_version_digest_mismatch_is_rejected_before_updates(self):
        self.state["tags"]["ghcr.io/test/runloom-docs:0.10.0"] = OLD
        self.assertNotEqual(self.run_release().returncode, 0)
        self.assert_no_mutation()

    def test_partial_failure_keeps_github_latest_until_retry_succeeds(self):
        self.assertNotEqual(self.run_release(FAIL_IMAGE="ghcr.io/test/runloom-docs:latest").returncode, 0)
        self.assertEqual(self.state["latest"], "v0.9.0")
        self.assertEqual(self.state["tags"]["ghcr.io/test/runloom-auth:latest"], DIGEST)
        result = self.run_release()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.state["latest"], "v0.10.0")

    def test_publication_defers_latest_for_stable_and_prerelease(self):
        self.state["releases"] = []
        destination = self.root / "release"
        destination.mkdir()
        for version in ("0.10.0", "0.11.0-rc.1"):
            self.version = version
            for chart in ("runloom", "runloom-ingress"):
                (destination / f"{chart}-{version}.tgz").write_text("chart fixture")
            result = self.run_release("publish")
            self.assertEqual(result.returncode, 0, result.stderr)
        updates = [c for c in self.calls() if c[:3] == ["gh", "release", "edit"]]
        self.assertEqual(len(updates), 2)
        self.assertTrue(all("--latest=false" in c for c in updates))


if __name__ == "__main__":
    unittest.main()
