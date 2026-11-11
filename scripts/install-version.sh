#!/usr/bin/env bash
set -euo pipefail
usage() {
  cat <<'EOF'
Usage: install-version.sh VERSION [--no-forward]
Example: ./install-version.sh 0.1.0-rc.1
Creates or reuses the local kind cluster runloom-rc, installs the published
chart, configures login, runs Helm tests, then serves Studio on localhost:4210.
Use --no-forward to exit after verification. Override the cluster, Studio port
and wait time with K8S_CLUSTER, K8S_PORT and K8S_TIMEOUT (default 15m).
Requires Docker, kind, kubectl, Helm, Node.js, curl and tar.
EOF
}
if [[ ${1:-} == --help || ${1:-} == -h ]]; then usage; exit 0; fi
if [[ $# -lt 1 || $# -gt 2 || ( $# -eq 2 && $2 != --no-forward ) ]]; then usage >&2; exit 2; fi
version=${1#v}
number='(0|[1-9][0-9]*)'
identifier="($number|[0-9]*[A-Za-z-][0-9A-Za-z-]*)"
pattern="^$number\\.$number\\.$number(-$identifier(\\.$identifier)*)?$"
if [[ ! $version =~ $pattern || ${#version} -gt 128 ]]; then echo 'Expected a release version such as 0.1.0-rc.1.' >&2; exit 2; fi
for tool in docker kind kubectl helm node curl tar; do
  command -v "$tool" >/dev/null || { echo "Missing required command: $tool" >&2; exit 1; }
done
export K8S_CLUSTER=${K8S_CLUSTER:-runloom-rc} K8S_PORT=${K8S_PORT:-4210} K8S_TIMEOUT=${K8S_TIMEOUT:-15m}
export K8S_NAMESPACE=runloom K8S_RELEASE=runloom K8S_DATABASE=postgres
work=$(mktemp -d "${TMPDIR:-/tmp}/runloom-install.XXXXXXXX")
trap 'rm -rf -- "$work"' EXIT
cd "$work"
echo "Downloading Runloom $version for kind-$K8S_CLUSTER"
curl --fail --silent --show-error --location --retry 3 \
  "https://codeload.github.com/itsneel/runloom/tar.gz/refs/tags/v$version" |
  tar -xz --strip-components=1 "runloom-$version/scripts"
mkdir charts
helm pull oci://ghcr.io/itsneel/charts/runloom --version "$version" --untar --untardir charts

node --input-type=module <<'JS'
import { writeFileSync } from 'node:fs';
import { configuration, execute, command } from './scripts/local-k8s.mjs';
import { localServiceValues, provisionLocalDatabases } from './scripts/local-k8s-services.mjs';
import { provisionLocalConsumer } from './scripts/local-k8s-consumer.mjs';
import { ensureLocalAdmin } from './scripts/local-k8s-open.mjs';
import { localAccountTable } from './scripts/local-account-table.mjs';

const c = configuration();
execute('cluster', c);
const helm = ['--kube-context', c.context, '--namespace', c.namespace];
const releases = JSON.parse(command(c.helm, ['list', ...helm, '--deployed', '--failed',
  '--pending', '--uninstalling', '--uninstalled', '--filter', '^runloom$', '-o', 'json'], true));
const current = releases[0];
const origin = 'http://localhost:' + c.port;
let values = {};
if (current) {
  if (!['deployed', 'failed'].includes(current.status)) {
    throw new Error('Existing release is ' + current.status + '. Check helm status before retrying.');
  }
  const plan = execute('open', c);
  const installed = JSON.parse(command(c.helm, ['get', 'values', c.release, ...helm, '--all', '-o', 'json'], true));
  if (plan.fullname !== c.release || installed.database.driver !== 'postgres' ||
      installed.databaseSetup?.existingSecret !== 'runloom-local-bootstrap') {
    throw new Error('Existing release uses different database settings. Choose a fresh K8S_CLUSTER.');
  }
  values = JSON.parse(command(c.helm, ['get', 'values', c.release, ...helm, '-o', 'json'], true)) || {};
  delete values.images;
  if (values.docs) delete values.docs.image;
}
writeFileSync('previous-values.json', JSON.stringify(values), {mode: 0o600});
writeFileSync('local-values.json', JSON.stringify(localServiceValues(c.release)), {mode: 0o600});
provisionLocalDatabases(c, {fullname: c.release, database: 'postgres'});
command(c.helm, ['upgrade', '--install', c.release, 'charts/runloom', ...helm, '--reset-values',
  '-f', 'previous-values.json', '-f', 'local-values.json', '--set-string', 'database.driver=postgres',
  '--set-string', 'auth.publicURL=' + origin, '--wait', '--timeout', c.timeout]);
const plan = execute('open', c);
await provisionLocalConsumer(c, plan);
const accounts = await ensureLocalAdmin(c, plan);
command(c.helm, ['test', c.release, ...helm, '--logs', '--timeout', c.timeout]);
console.log(localAccountTable(accounts, origin));
JS
if [[ ${2:-} != --no-forward ]]; then
  echo "Open http://localhost:$K8S_PORT (Ctrl+C stops forwarding)."
  kubectl --context "kind-$K8S_CLUSTER" -n runloom port-forward --address 127.0.0.1 svc/runloom-ui "$K8S_PORT:80"
fi
