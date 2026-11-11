"""Exercise the published-release catalog with a local API fixture and real Helm clients."""
import functools
import http.server
import json
import os
from pathlib import Path
import subprocess
import tempfile
import threading

root = Path(__file__).resolve().parents[2]
charts = ("runloom", "runloom-ingress")
versions = ("1.0.0", "1.1.0-rc.1", "1.1.0")
with tempfile.TemporaryDirectory() as tmp:
    work = Path(tmp)
    site = work / "pages" / "charts"
    env = dict(os.environ, PATH=tmp + os.pathsep + os.environ["PATH"],
               HELM_CACHE_HOME=tmp + "/cache", HELM_CONFIG_HOME=tmp + "/config",
               HELM_DATA_HOME=tmp + "/data", GITHUB_REPOSITORY="example/runloom",
               VERSION=versions[-1], FIXTURES=tmp)

    def run(*args, **kwargs):
        return subprocess.run(args, cwd=work, env=env, text=True, check=True,
                              stdout=subprocess.PIPE, **kwargs).stdout

    releases = []
    for version in versions:
        assets = []
        for chart in charts:
            run("helm", "package", root / "charts" / chart, "--version", version)
            name = f"{chart}-{version}.tgz"
            asset_id = str(len(releases) * 2 + len(assets) + 1)
            (work / asset_id).write_bytes((work / name).read_bytes())
            assets.append({"id": int(asset_id), "name": name})
        releases.append({"tag_name": "v" + version, "draft": False, "assets": assets})
    releases.append({"tag_name": "v2.0.0", "draft": True,
                     "assets": [{"id": 99, "name": "runloom-2.0.0.tgz"}]})
    releases[0]["assets"].append({"id": 100, "name": "unrelated.tgz"})
    # gh --paginate emits one array per API page.
    (work / "list").write_text(json.dumps(releases[:2]) + "\n" + json.dumps(releases[2:]))
    fake = work / "gh"
    fake.write_text("""#!/bin/sh
set -eu
case "$*" in
  'api --paginate repos/example/runloom/releases?per_page=100') cat "$FIXTURES/list" ;;
  'api -H Accept: application/octet-stream repos/example/runloom/releases/assets/'*) cat "$FIXTURES/${4##*/}" ;;
  *) exit 1 ;;
esac
""")
    fake.chmod(0o755)
    run("bash", root / "scripts/helm-repository.sh", site)
    assert sorted(p.name for p in site.glob("*.tgz")) == sorted(
        f"{c}-{v}.tgz" for c in charts for v in versions)

    handler = functools.partial(http.server.SimpleHTTPRequestHandler, directory=site.parent)
    with http.server.ThreadingHTTPServer(("127.0.0.1", 0), handler) as server:
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            run("helm", "repo", "add", "runloom", f"http://127.0.0.1:{server.server_port}/charts")
            run("helm", "repo", "update", "runloom")
            catalog = json.loads(run("helm", "search", "repo", "runloom/", "--versions", "--devel", "-o", "json"))
            assert {(row["name"], row["version"]) for row in catalog} == {
                ("runloom/" + c, v) for c in charts for v in versions}
            downloads = work / "downloads"
            downloads.mkdir()
            for chart in charts:
                for version in versions:
                    run("helm", "pull", "runloom/" + chart, "--version", version, "-d", downloads)
                    name = f"{chart}-{version}.tgz"
                    assert (work / name).read_bytes() == (downloads / name).read_bytes()
        finally:
            server.shutdown()
            thread.join()
    print("Helm discovered and pulled both charts across stable and prerelease versions.")
