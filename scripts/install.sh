#!/bin/sh
# Install the published CLI, official provider tools, PATH entry, and Agent Skill.
set -eu
umask 077
fail() { printf '%s\n' "$*" >&2; exit 1; }
[ "$#" -le 1 ] || fail 'Usage: sh install.sh [INSTALL_DIRECTORY]'
case "$(uname -s):$(uname -m)" in
  Darwin:arm64) ;;
  *) fail 'Ship currently supports installation on macOS Apple Silicon only.' ;;
esac
case "${SHELL##*/}" in zsh|bash) ;; *) fail 'Automatic PATH setup currently supports zsh and bash. Run from one of these shells.' ;; esac
for tool in curl tar shasum node npm; do
  command -v "$tool" >/dev/null 2>&1 || fail "Install $tool first. Ship needs Node.js 24+ and npm: https://nodejs.org/en/download"
done
node -e 'if (Number(process.versions.node.split(".")[0]) < 24) process.exit(1)' || fail 'Ship needs Node.js 24+; install it from https://nodejs.org/en/download'

version=v0.1.0-alpha.1
destination=${1:-"$HOME/.local/share/ship"}
case "$destination" in /*) ;; *) fail 'INSTALL_DIRECTORY must be an absolute path.' ;; esac
bin_link="$HOME/.local/bin/ship"
agent_link="$HOME/.agents/skills/ship"
claude_link="$HOME/.claude/skills/ship"
check_link() {
  if [ -e "$1" ] || [ -L "$1" ]; then
    [ -L "$1" ] && [ "$(readlink "$1")" = "$2" ] || fail "Already exists and is not managed by this installation: $1"
  fi
}
check_link "$bin_link" "$destination/bin/ship"
check_link "$agent_link" "$destination/skills/ship"
check_link "$claude_link" "$destination/skills/ship"
existing=$(command -v ship || true)
case "$existing" in ''|"$bin_link"|"$destination/bin/ship") ;; *) fail "Another ship command exists at $existing; resolve the name conflict first." ;; esac
if [ -e "$destination" ] || [ -L "$destination" ]; then
  [ ! -L "$destination" ] && [ -f "$destination/VERSION" ] && [ "$(cat "$destination/VERSION")" = "$version" ] || fail "Installation directory already exists: $destination. Keep it as a backup or choose an empty directory. Your ~/.ship data is separate."
  [ "$("$destination/bin/ship" --version)" = "ship $version" ] || fail 'Existing installation is incomplete; keep it as a backup and install again.'
else
  parent=$(dirname -- "$destination")
  mkdir -p "$parent"
  stage=$(mktemp -d "$parent/.ship-install.XXXXXX")
  trap 'rm -rf -- "$stage"' EXIT HUP INT TERM
  asset="ship-$version-darwin-arm64.tar.gz"
  base="https://github.com/unix2dos/ship/releases/download/$version"
  curl --fail --location --show-error --connect-timeout 15 --max-time 180 "$base/$asset" -o "$stage/$asset"
  curl --fail --location --show-error --connect-timeout 15 --max-time 30 "$base/$asset.sha256" -o "$stage/$asset.sha256"
  (cd "$stage" && shasum -a 256 -c "$asset.sha256")
  mkdir "$stage/unpacked"
  tar -xzf "$stage/$asset" -C "$stage/unpacked" --strip-components=1
  [ "$(cat "$stage/unpacked/VERSION")" = "$version" ] || fail 'Unexpected package version.'
  [ "$("$stage/unpacked/bin/ship" --version)" = "ship $version" ] || fail 'Downloaded CLI did not pass its startup check.'
  (cd "$stage/unpacked/tools" && npm ci --no-audit --no-fund)
  for tool in railway neon vercel; do
    VERCEL_TELEMETRY_DISABLED=1 NO_UPDATE_NOTIFIER=1 "$stage/unpacked/tools/node_modules/.bin/$tool" --version
  done
  [ ! -e "$destination" ] && [ ! -L "$destination" ] || fail 'Installation destination appeared during installation; nothing was replaced.'
  mv "$stage/unpacked" "$destination"
  rm -rf -- "$stage"
  trap - EXIT HUP INT TERM
fi

mkdir -p "$HOME/.local/bin" "$HOME/.agents/skills" "$HOME/.claude/skills"
[ -L "$bin_link" ] || ln -s "$destination/bin/ship" "$bin_link"
[ -L "$agent_link" ] || ln -s "$destination/skills/ship" "$agent_link"
[ -L "$claude_link" ] || ln -s "$destination/skills/ship" "$claude_link"
path_line='case ":$PATH:" in *":$HOME/.local/bin:"*) ;; *) export PATH="$HOME/.local/bin:$PATH" ;; esac # ship'
add_path() {
  if ! [ -f "$1" ] || ! grep -Fqx "$path_line" "$1"; then
    printf '\n%s\n' "$path_line" >> "$1"
  fi
}
case "${SHELL##*/}" in
  zsh)
    # zsh uses ZDOTDIR when configured; do not redirect an existing setup to HOME.
    shell_dir=${ZDOTDIR:-"$HOME"}
    mkdir -p "$shell_dir"
    add_path "$shell_dir/.zshrc"
    add_path "$shell_dir/.zprofile"
    ;;
  bash)
    add_path "$HOME/.bashrc"
    if [ -f "$HOME/.bash_profile" ]; then add_path "$HOME/.bash_profile"
    elif [ -f "$HOME/.bash_login" ]; then add_path "$HOME/.bash_login"
    else add_path "$HOME/.profile"; fi
    ;;
esac
printf '\nShip %s installed.\nCLI: %s\nSkill (Codex): %s\nSkill (Claude Code): %s\n' "$version" "$bin_link" "$agent_link" "$claude_link"
printf '\nOpen a new terminal, or run this once in the current terminal:\n  export PATH="$HOME/.local/bin:$PATH"\nThen: ship --version\n      ship serve --open\n\nThe Skill will be available on your next Agent turn; restart the Agent if it is not listed.\nGuide: %s/docs/FIRST-TRY.md\n' "$destination"
