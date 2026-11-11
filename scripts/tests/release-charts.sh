#!/usr/bin/env bash
# Exercise the customer-facing archives, including production overrides.
set -euo pipefail
out=${RELEASE_DIR:-dist/release}
chart="$out/runloom-$VERSION.tgz"
ingress="$out/runloom-ingress-$VERSION.tgz"
work=$(mktemp -d)
trap 'rm -r "$work"' EXIT

awk -v registry="$REGISTRY" -v version="$VERSION" \
  '{print registry "/" $1 ":" version "@" $2}' "$out/image-digests.tsv" | sort > "$work/expected"
helm show values "$chart" > "$work/defaults.yaml"
tar -xOzf "$chart" runloom/values-production.yaml > "$work/production.yaml"
for values in "$work/defaults.yaml" "$work/production.yaml"; do
  yq -r '(.images[], .docs.image) | .repository + ":" + .tag' "$values" | sort > "$work/actual"
  diff -u "$work/expected" "$work/actual"
  helm template release-test "$chart" -f "$values" >/dev/null
done
helm template ingress-test "$ingress" --set nginx.enabled=true >/dev/null
[[ $(helm show chart "$chart" | yq -r '.appVersion') == "$VERSION" ]]
[[ $(helm show chart "$ingress" | yq -r '.appVersion') == "$(yq -r '.appVersion' charts/runloom-ingress/Chart.yaml)" ]]
