#!/bin/sh
# Build an explicit, credential-free release archive. Publishing is a separate step.
set -eu
[ "$#" -ge 1 ] && [ "$#" -le 2 ] || { echo 'Usage: sh scripts/package.sh VERSION [OUTPUT_DIRECTORY]' >&2; exit 1; }
version=$1
case "$version" in v[0-9]*) ;; *) echo 'Version must start with v and a digit.' >&2; exit 1;; esac
case "$version" in *[!a-zA-Z0-9.-]*) echo 'Invalid version.' >&2; exit 1;; esac
repo=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
out=${2:-"$repo/validation/artifacts/releases/$version"}
mkdir -p "$out"
out=$(CDPATH= cd -- "$out" && pwd)
stage=$(mktemp -d)
trap 'rm -rf -- "$stage"' EXIT HUP INT TERM
bundle="$stage/ship"
mkdir -p "$bundle/bin" "$bundle/tools" "$bundle/docs/research" "$bundle/skills/ship" "$bundle/validation/results"
(cd "$repo" && CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -buildvcs=false -ldflags "-X main.version=$version" -o "$bundle/bin/ship" ./cmd/ship)
printf '%s\n' "$version" > "$bundle/VERSION"
git -C "$repo" rev-parse HEAD > "$bundle/SOURCE_COMMIT"
cp "$repo/LICENSE" "$bundle/"
cp "$repo/validation/cloud-tools/package.json" "$repo/validation/cloud-tools/package-lock.json" "$bundle/tools/"
cp "$repo/docs/ALPHA.md" "$repo/docs/FIRST-TRY.md" "$bundle/docs/"
cp "$repo/docs/research/2026-09-16-free-plan-boundaries.md" "$bundle/docs/research/"
cp "$repo/skills/ship/SKILL.md" "$bundle/skills/ship/"
cp "$repo/validation/VERCEL-RESULTS.md" "$bundle/validation/"
cp "$repo/validation/results/vercel-secret-update-diagnosis.json" "$repo/validation/results/ship-vercel-acceptance.json" "$bundle/validation/results/"
asset="ship-$version-darwin-arm64.tar.gz"
COPYFILE_DISABLE=1 tar -czf "$out/$asset" -C "$stage" ship
(cd "$out" && shasum -a 256 "$asset" > "$asset.sha256")
cp "$repo/scripts/install.sh" "$out/install.sh"
printf '%s\n' "$out"
