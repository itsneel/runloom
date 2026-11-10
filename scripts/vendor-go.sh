#!/bin/sh
# Prepare standalone service vendors without persisting sibling-source replacements.
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
GO=${GO:-go}
export GOWORK=off
stage=$(mktemp -d "${TMPDIR:-/tmp}/runloom-vendor.XXXXXX")
trap 'rm -rf "$stage"' EXIT
trap 'exit 1' HUP INT TERM
for project in server/auth server/workflows server/management server/gateway server/assistant tools/database-setup; do
    echo "Refreshing Go dependencies: $project"
    mkdir -p "$stage/$project"
    cp "$root/$project/go.mod" "$stage/$project/go.mod"
    cp "$root/$project/go.sum" "$stage/$project/go.sum"
    modfile="$stage/$project/go.mod"
    for sdk in go-auth go-observability; do
        (cd "$root/$project" && "$GO" mod edit -modfile="$modfile" "-replace=github.com/example/runloom/sdk/$sdk=$root/sdk/$sdk")
    done
    (cd "$root/$project" && "$GO" mod vendor -modfile="$modfile" -o "$stage/$project/vendor")
    # Local SDKs are copied into vendor, so standalone consumers need no replacements.
    awk '
        /^# github.com\/example\/runloom\/sdk\/go-(auth|observability)( |$)/ {
            if ($3 == "=>") next
            sub(/ => .*/, "")
        }
        { print }
    ' "$stage/$project/vendor/modules.txt" > "$stage/$project/modules.txt"
    mv "$stage/$project/modules.txt" "$stage/$project/vendor/modules.txt"
done
# Publish only after every module has been successfully prepared.
for project in server/auth server/workflows server/management server/gateway server/assistant tools/database-setup; do
    rm -rf "$root/$project/vendor"
    mv "$stage/$project/vendor" "$root/$project/vendor"
done
