#!/usr/bin/env bash
# Build the Pages repository from published GitHub Release assets, including prereleases.
set -euo pipefail
site=${1:?Expected an empty chart repository directory}
: "${VERSION:?}" "${GITHUB_REPOSITORY:?}"
mkdir -p "$site"
[[ -z $(ls -A "$site") ]] || { echo 'Chart repository directory must be empty' >&2; exit 1; }
assets=$(mktemp)
trap 'rm -f "$assets"' EXIT

gh api --paginate "repos/$GITHUB_REPOSITORY/releases?per_page=100" | jq -r '
  .[] | select(.draft == false)
  | .tag_name as $tag | select($tag | test("^v[0-9][0-9A-Za-z.-]*$"))
  | ($tag | ltrimstr("v")) as $version
  | .assets[] | select(.name == ("runloom-" + $version + ".tgz") or .name == ("runloom-ingress-" + $version + ".tgz"))
  | [.id, .name] | @tsv' > "$assets"

while IFS=$'\t' read -r id filename; do
  gh api -H 'Accept: application/octet-stream' "repos/$GITHUB_REPOSITORY/releases/assets/$id" > "$site/$filename"
  metadata=$(helm show chart "$site/$filename" | yq -r '.name + "-" + .version + ".tgz"')
  [[ $filename == "$metadata" ]] || { echo "Chart metadata does not match $filename" >&2; exit 1; }
done < "$assets"

for chart in runloom runloom-ingress; do
  [[ -s "$site/$chart-$VERSION.tgz" ]] || { echo "Missing published $chart $VERSION" >&2; exit 1; }
done
helm repo index "$site"
