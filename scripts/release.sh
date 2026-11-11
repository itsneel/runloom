#!/usr/bin/env bash
# Manual GitHub release. The package subcommand also runs locally with a digest inventory.
set -euo pipefail

die() { echo "$*" >&2; exit 1; }
number='(0|[1-9][0-9]*)'
identifier="($number|[0-9]*[A-Za-z-][0-9A-Za-z-]*)"
version_pattern="^${number}\\.${number}\\.${number}(-${identifier}(\\.${identifier})*)?$"
[[ ${VERSION:-} =~ $version_pattern && ${#VERSION} -le 128 ]] || die 'Expected a Docker-compatible semantic version such as 1.2.3 or 1.2.3-rc.1.'
tag="v$VERSION"
out=${RELEASE_DIR:-dist/release}
values=charts/runloom/values.yaml

inventory() { yq -r '.images[].repository, .docs.image.repository' "$values"; }

release_state() {
  gh api --paginate "repos/$GITHUB_REPOSITORY/releases?per_page=100" \
    --jq '.[] | [.tag_name, .draft] | @tsv' | awk -v tag="$tag" '$1 == tag {print ($2 == "true" ? "draft" : "published")}'
}

reserve() {
  local state
  state=$(release_state)
  [[ $state != published ]] || die "Release $tag already exists."
  if git show-ref --verify --quiet "refs/tags/$tag"; then
    [[ $(git rev-parse "refs/tags/$tag^{commit}") == "$GITHUB_SHA" ]] || die "Tag $tag belongs to another commit."
  else
    gh api --method POST "repos/$GITHUB_REPOSITORY/git/refs" -f ref="refs/tags/$tag" -f sha="$GITHUB_SHA" --silent
  fi
}

stage() {
  local images name ref
  images=$(inventory)
  for name in $images; do
    ref="$REGISTRY/$name:build-$GITHUB_RUN_ID-$ARCH"
    docker tag "$name:local" "$ref"
    docker push --platform "linux/$ARCH" "$ref"
  done
}

promote() {
  local images name ref digest state
  state=$(release_state)
  [[ $state != published ]] || die "Release $tag already exists."
  mkdir -p "$out"
  : > "$out/image-digests.tsv"
  images=$(inventory)
  for name in $images; do
    ref="$REGISTRY/$name"
    docker buildx imagetools create --tag "$ref:$VERSION" \
      --annotation "index:org.opencontainers.image.source=$GITHUB_SERVER_URL/$GITHUB_REPOSITORY" \
      --annotation "index:org.opencontainers.image.revision=$GITHUB_SHA" \
      "$ref:build-$GITHUB_RUN_ID-amd64" "$ref:build-$GITHUB_RUN_ID-arm64"
    digest=$(docker buildx imagetools inspect "$ref:$VERSION" | awk '$1 == "Digest:" && !found {print $2; found=1}')
    [[ $digest =~ ^sha256:[a-f0-9]{64}$ ]] || die "Missing registry digest for $name."
    docker buildx imagetools inspect --raw "$ref@$digest" | jq -e \
      '[.manifests[].platform | [.os,.architecture]] | sort == [["linux","amd64"],["linux","arm64"]]' >/dev/null
    printf '%s\t%s\n' "$name" "$digest" >> "$out/image-digests.tsv"
  done
}

package() {
  local charts="$out/source" keys key path name digest file
  mkdir -p "$charts"
  local chart
  for chart in runloom runloom-ingress; do
    mkdir -p "$charts/$chart"
    cp -R "charts/$chart/." "$charts/$chart/"
    yq -i '.annotations."org.opencontainers.image.source" = strenv(GITHUB_SERVER_URL) + "/" + strenv(GITHUB_REPOSITORY)' "$charts/$chart/Chart.yaml"
  done
  keys=$(yq -r '.images | keys | .[]' "$values")
  for key in $keys docs; do
    path=".images[\"$key\"]"
    if [[ $key == docs ]]; then path='.docs.image'; fi
    name=$(yq -r "$path.repository" "$values")
    digest=$(awk -v name="$name" '$1 == name {print $2}' "$out/image-digests.tsv")
    [[ $digest =~ ^sha256:[a-f0-9]{64}$ ]] || die "Expected one digest for $name."
    for file in values.yaml values-production.yaml; do
      REPOSITORY="$REGISTRY/$name" IMAGE_TAG="$VERSION@$digest" yq -i \
        "$path.repository = strenv(REPOSITORY) | $path.tag = strenv(IMAGE_TAG)" "$charts/runloom/$file"
    done
  done
  helm lint --strict "$charts/runloom"
  helm lint --strict "$charts/runloom" -f "$charts/runloom/values-production.yaml"
  helm lint --strict "$charts/runloom-ingress" --set nginx.enabled=true
  helm package "$charts/runloom" --version "$VERSION" --app-version "$VERSION" --destination "$out"
  # Ingress appVersion describes the upstream NGINX controller.
  helm package "$charts/runloom-ingress" --version "$VERSION" --destination "$out"
}

publish() {
  local chart state prerelease=false
  state=$(release_state)
  [[ $state != published ]] || die "Release $tag already exists."
  for chart in runloom runloom-ingress; do
    helm push "$out/$chart-$VERSION.tgz" "oci://$REGISTRY/charts"
    mkdir -p "$out/verified"
    helm pull "oci://$REGISTRY/charts/$chart" --version "$VERSION" --destination "$out/verified"
    cmp "$out/$chart-$VERSION.tgz" "$out/verified/$chart-$VERSION.tgz"
  done
  [[ $VERSION != *-* ]] || prerelease=true
  if [[ $state != draft ]]; then
    cat > "$out/registry-notes.md" <<NOTES
After the Helm repository job completes, discover the published charts with Helm.

\`\`\`bash
helm repo add runloom "${PAGES_URL%/}/charts"
helm repo update runloom
helm search repo runloom/ --versions --devel
\`\`\`

Pull the versioned charts directly from GitHub Container Registry:

\`\`\`bash
helm pull oci://$REGISTRY/charts/runloom --version $VERSION
helm pull oci://$REGISTRY/charts/runloom-ingress --version $VERSION
\`\`\`

The application chart pins the Linux AMD64 and ARM64 image indexes. The attached image-digests.tsv lists each image and its digest.
NOTES
    gh release create "$tag" --verify-tag --draft --prerelease="$prerelease" --title "Runloom $VERSION" \
      --generate-notes --notes-file "$out/registry-notes.md"
  fi
  gh release upload "$tag" "$out/runloom-$VERSION.tgz" "$out/runloom-ingress-$VERSION.tgz" "$out/image-digests.tsv" --clobber
  gh release edit "$tag" --draft=false --prerelease="$prerelease"
}

case ${1:-} in
  reserve) reserve ;;
  stage) stage ;;
  promote) promote ;;
  package) package ;;
  publish) publish ;;
  *) die 'Expected reserve, stage, promote, package, or publish.' ;;
esac
