#!/bin/sh
# Source installation into an empty user-owned directory; no sudo or shell changes.
set -eu
umask 077

fail() { printf '%s\n' "$*" >&2; exit 1; }
[ "$#" -le 1 ] || fail 'Usage: sh scripts/install.sh [INSTALL_DIRECTORY]'
case "$(uname -s):$(uname -m)" in
  Darwin:arm64) ;;
  *) fail 'This alpha installer has only been validated on macOS arm64.' ;;
esac
for tool in go node npm; do
  command -v "$tool" >/dev/null 2>&1 || fail "Install $tool first; see docs/ALPHA.md."
done
[ "$(go env CGO_ENABLED)" = 1 ] || fail 'macOS Keychain support requires CGO_ENABLED=1 and Apple Command Line Tools.'
node -e 'const [major,minor]=process.versions.node.split(".").map(Number); if (major<20 || (major===20 && minor<19)) process.exit(1)' || fail 'Node.js 20.19+ is required.'

repo=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
destination=${1:-"$HOME/.local/share/ship"}
case "$destination" in /*) ;; *) fail 'INSTALL_DIRECTORY must be an absolute path.' ;; esac
[ ! -e "$destination" ] && [ ! -L "$destination" ] || fail 'Installation directory already exists. Choose a new directory; this installer never overwrites an installation.'
parent=$(dirname -- "$destination")
mkdir -p "$parent"
stage=$(mktemp -d "$parent/.ship-install.XXXXXX")
trap 'rm -rf -- "$stage"' EXIT HUP INT TERM
mkdir -p "$stage/bin" "$stage/tools" "$stage/docs" "$stage/skills/ship"
cp "$repo/validation/cloud-tools/package.json" "$repo/validation/cloud-tools/package-lock.json" "$stage/tools/"
(cd "$repo" && go build -buildvcs=false -o "$stage/bin/ship" ./cmd/ship)
npm ci --prefix "$stage/tools" --no-audit --no-fund
"$stage/tools/node_modules/.bin/railway" --version
"$stage/tools/node_modules/.bin/neon" --version
cp "$repo/docs/ALPHA.md" "$stage/docs/"
mkdir -p "$stage/docs/research"
cp "$repo/docs/research/2026-09-16-free-plan-boundaries.md" "$stage/docs/research/"
cp "$repo/skills/ship/SKILL.md" "$stage/skills/ship/"
"$stage/bin/ship" help >/dev/null
[ ! -e "$destination" ] && [ ! -L "$destination" ] || fail 'Installation destination appeared during installation; nothing was replaced.'
mv "$stage" "$destination"
trap - EXIT HUP INT TERM
printf '\nShip installed at: %s\nStart: "%s/bin/ship" serve --open\nGuide: %s/docs/ALPHA.md\n' "$destination" "$destination" "$destination"
