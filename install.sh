#!/bin/sh
# Cully installer — downloads a prebuilt binary. Run cully setup afterwards
# for the complete local stack. Older agent copies become symlinks to this single binary.
# Use --from-source to explicitly build with Go.
#
#   curl -fsSL https://cully.net/install.sh | sh -s -- --agent codex --mcp-url http://127.0.0.1:8080/mcp
#
# Env overrides: CULLY_VERSION (e.g. v0.1.0), CLAUDE_CONFIG_DIR, CODEX_HOME,
# CURSOR_CONFIG_DIR. Add --oauth only when the MCP server requires OAuth.
set -eu

REPO="tavric-labs/cully"

agent=""
mcp_url=""
oauth=false
from_source=false
while [ "$#" -gt 0 ]; do
  case "$1" in
    --agent)
      [ "$#" -ge 2 ] || { echo '--agent requires a value' >&2; exit 2; }
      agent="$2"; shift 2 ;;
    --mcp-url)
      [ "$#" -ge 2 ] || { echo '--mcp-url requires a value' >&2; exit 2; }
      mcp_url="$2"; shift 2 ;;
    --oauth) oauth=true; shift ;;
    --from-source) from_source=true; shift ;;
    *) echo "unknown installer option: $1" >&2; exit 2 ;;
  esac
done
[ "$oauth" = false ] || [ -n "$mcp_url" ] || { echo '--oauth requires --mcp-url' >&2; exit 2; }
case "$agent" in
  ""|claude|codex|cursor|all) ;;
  *) echo 'choose --agent claude, codex, cursor, or all' >&2; exit 2 ;;
esac
# One CLI location for every agent; refresh older managed copies below.
BIN_DIR="$HOME/.local/bin"
current_cully="$(command -v cully 2>/dev/null || true)"

die() { printf '\033[31mx\033[0m %s\n' "$1" >&2; exit 1; }
say() { printf '\033[36m==>\033[0m %s\n' "$1"; }

say "Installation plan: install the CLI in $BIN_DIR and link agent commands to that one binary."
if [ -n "$current_cully" ]; then
  say "Current PATH selects: $current_cully ($("$current_cully" version 2>/dev/null || echo 'version unavailable'))"
else
  say 'No existing Cully command found on PATH.'
fi
say 'The installer prints version changes, preserved copies, and any terminal refresh needed.'

command -v curl >/dev/null 2>&1 || die "curl is required."
command -v tar  >/dev/null 2>&1 || die "tar is required."

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"
raw_os="$os"
raw_arch="$arch"
case "$arch" in
  x86_64|amd64)  arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *) die "unsupported arch: $raw_arch (supported: amd64, arm64)" ;;
esac
case "$os" in
  darwin|linux) ;;
  *) die "unsupported OS: $raw_os (supported: darwin, linux)" ;;
esac

ver="${CULLY_VERSION:-latest}"
asset="cully_${os}_${arch}.tar.gz"
if [ "$ver" = "latest" ]; then
  url="https://github.com/$REPO/releases/latest/download/$asset"
else
  url="https://github.com/$REPO/releases/download/$ver/$asset"
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
say "Detected platform: $raw_os/$raw_arch -> $os/$arch"
if [ "$from_source" = true ]; then
  command -v go >/dev/null 2>&1 || die "Source installation requires Go 1.26 or newer."
  ref="$ver"
  [ "$ref" != latest ] || ref=main
  say "Building Cully from $ref; downloading the Go toolchain and dependencies can take several minutes"
  GOBIN="$tmp" go install -v "github.com/mcp-runtime/cully/cmd/cully@$ref" || die "source install failed for $ref"
else
  say "Downloading $asset ($ver)"
  if curl -fL --progress-bar --connect-timeout 10 --max-time 120 --speed-limit 1024 --speed-time 30 "$url" -o "$tmp/c.tar.gz"; then
    sums_url="$(dirname "$url")/checksums.txt"
    say "Verifying download"
    if curl -fsSL --connect-timeout 10 --max-time 30 "$sums_url" -o "$tmp/checksums.txt"; then
      expected="$(awk -v asset="$asset" '$2 == asset {print $1; exit}' "$tmp/checksums.txt")"
      if [ -n "$expected" ]; then
        if command -v sha256sum >/dev/null 2>&1; then
          actual="$(sha256sum "$tmp/c.tar.gz" | awk '{print $1}')"
        else
          actual="$(shasum -a 256 "$tmp/c.tar.gz" | awk '{print $1}')"
        fi
        [ "$actual" = "$expected" ] || die "checksum mismatch for $asset (expected $expected, got $actual) — aborting install"
      else
        say "warning: no checksum entry for $asset — skipping verification"
      fi
    else
      say "warning: could not fetch checksums.txt — skipping verification"
    fi

    tar -xzf "$tmp/c.tar.gz" -C "$tmp" || die "extract failed"
    [ -f "$tmp/cully" ] || die "archive did not contain the cully binary"
  else
    die "Could not download $asset ($ver). Check your connection and the release at https://github.com/$REPO/releases, then retry. To build with Go instead, rerun with: sh -s -- --from-source"
  fi
fi
[ -f "$tmp/cully" ] || die "installer did not produce the cully binary"

tmp_bin="$tmp/cully"
new_ver="$("$tmp_bin" version 2>/dev/null)" || die 'Downloaded binary could not report its version.'
case "$new_ver" in 'cully '*) ;; *) die 'Downloaded executable did not identify as Cully.' ;; esac
say "Downloaded version: $new_ver"


# Install the single real binary atomically; replace a previous link itself,
# leaving its old target untouched.
install_binary() {
  destination="$1"
  [ ! -d "$destination" ] || die "Installation path is a directory: $destination"
  directory=$(dirname "$destination")
  mkdir -p "$directory" || die "Cannot create installation directory $directory"
  staged=$(mktemp "$directory/.cully-install.XXXXXX") || die "Cannot write to $directory"
  if ! install -m 0755 "$tmp_bin" "$staged" || ! mv -f "$staged" "$destination"; then
    rm -f "$staged"
    die "Could not replace $destination"
  fi
  [ "$os" != darwin ] || xattr -d com.apple.quarantine "$destination" 2>/dev/null || true
}

if [ -x "$BIN_DIR/cully" ]; then
  say "Replacing $BIN_DIR/cully ($("$BIN_DIR/cully" version 2>/dev/null || echo 'version unavailable')) with $new_ver"
else
  say "Installing $new_ver in $BIN_DIR/cully"
fi
install_binary "$BIN_DIR/cully"
say "Installed binary -> $BIN_DIR/cully ($new_ver)"

# Agent paths contain only symlinks to the canonical binary. Migrate previous
# binaries and repair Cully links without changing their old targets or wrappers.
linked_paths=""
link_copy() {
  candidate="$1"
  create="${2:-existing}"
  case "$linked_paths" in
    *"
$candidate
"*) return 0 ;;
  esac
  linked_paths="$linked_paths
$candidate
"
  [ "$candidate" != "$BIN_DIR/cully" ] || return 0
  if [ -L "$candidate" ] && [ "$(readlink "$candidate")" = "$BIN_DIR/cully" ]; then
    say "Agent link already configured: $candidate -> $BIN_DIR/cully"
    return 0
  fi
  if [ -e "$candidate" ]; then
    if [ ! -f "$candidate" ] || [ ! -x "$candidate" ]; then
      say "Preserved custom path: $candidate (not an executable Cully file)"
      return 0
    fi
    candidate_version="$("$candidate" version 2>/dev/null || true)"
    case "$candidate_version" in
      'cully '*) ;;
      *) say "Preserved custom command: $candidate (does not identify as a Cully binary)"; return 0 ;;
    esac
  elif [ ! -L "$candidate" ]; then
    [ "$create" = create ] || [ -d "$(dirname "$(dirname "$candidate")")" ] || return 0
  fi
  case "$candidate" in
    "$HOME"/*) ;;
    *) say "Other installation outside your home: $candidate; use $BIN_DIR/cully or update it separately."; return 0 ;;
  esac
  link_dir=$(dirname "$candidate")
  if ! mkdir -p "$link_dir" || [ ! -w "$link_dir" ]; then
    say "Could not link $candidate: directory is not writable. Use $BIN_DIR/cully."
    return 0
  fi
  # Stage the link beside its destination so replacement is atomic.
  link_stage=$(mktemp -d "$link_dir/.cully-link.XXXXXX") || die "Cannot stage agent link in $link_dir"
  if ! ln -s "$BIN_DIR/cully" "$link_stage/cully" || ! mv -f "$link_stage/cully" "$candidate"; then
    rm -rf "$link_stage"
    die "Could not link $candidate to $BIN_DIR/cully"
  fi
  rmdir "$link_stage"
  say "Agent symlink: $candidate -> $BIN_DIR/cully ($new_ver); previous Cully copy or link replaced."
}
claude_link=existing
codex_link=existing
cursor_link=existing
case "$agent" in
  claude) claude_link=create ;;
  codex) codex_link=create ;;
  cursor) cursor_link=create ;;
  all) claude_link=create; codex_link=create; cursor_link=create ;;
esac
link_copy "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/bin/cully" "$claude_link"
link_copy "${CODEX_HOME:-$HOME/.codex}/bin/cully" "$codex_link"
link_copy "${CURSOR_CONFIG_DIR:-$HOME/.cursor}/bin/cully" "$cursor_link"
# Cover historical default directories when a custom agent directory is in use.
link_copy "$HOME/.claude/bin/cully"
link_copy "$HOME/.codex/bin/cully"
link_copy "$HOME/.cursor/bin/cully"
[ -z "$current_cully" ] || link_copy "$current_cully"

say 'Stopping any running advisor daemon so the next setup uses the new version.'
"$BIN_DIR/cully" _internal stop-daemon >/dev/null 2>&1 || true
PATH="$BIN_DIR:$PATH"
export PATH

# Add an idempotent PATH entry for future terminals without replacing settings.
profile="${ZDOTDIR:-$HOME}/.zshrc"
if [ -n "$profile" ]; then
  path_line='export PATH="$HOME/.local/bin:$PATH"'
  if [ ! -f "$profile" ] || ! grep -Fqx "$path_line" "$profile"; then
    if printf '\n%s\n' "$path_line" >> "$profile"; then
      say "Added Cully PATH entry to $profile for future terminals."
    else
      say "Could not update $profile; add $BIN_DIR to PATH manually."
    fi
  else
    say "Cully PATH entry already present in $profile."
  fi
fi
say 'Cully supports macOS/Linux native terminals with zsh + Oh My Zsh; setup installs the required shell and font configuration.'
say 'A piped installer cannot change the PATH or command cache in your current terminal.'
say 'To refresh this terminal, run: export PATH="$HOME/.local/bin:$PATH"; hash -r'
say 'Then verify: command -v cully; cully version'
say 'If a different version still appears, run type -a cully to check other paths, aliases, or functions.'
say "Expected binary: $BIN_DIR/cully; expected version: $new_ver"
say 'Only ~/.local/bin/cully contains the installed binary; agent symlinks use that same version on every upgrade.'
# Install a starter dev-workflow-rules skill with hint comments for you to fill
# in. An existing skill is never replaced; this asks nothing and is best effort.
"$BIN_DIR/cully" workflow </dev/null || say 'Could not install the starter dev-workflow-rules skill; run cully workflow later.'
if [ -n "$mcp_url" ]; then
  set -- setup --mcp-url "$mcp_url"
  [ -z "$agent" ] || set -- "$@" --agent "$agent"
  [ "$oauth" = false ] || set -- "$@" --oauth
  say 'Setting up coding agents and the advisor with your existing MCP server'
  "$BIN_DIR/cully" "$@"
else
  if [ -n "$agent" ]; then
    say "CLI installed. For the full local stack, start Docker and run: \"$BIN_DIR/cully\" setup --agent $agent --all"
    say "For a deployed stack, run: \"$BIN_DIR/cully\" setup --agent $agent --mcp-url URL"
  else
    say "CLI installed. For the full local stack, start Docker and run: \"$BIN_DIR/cully\" setup --all"
    say "For a deployed stack, run: \"$BIN_DIR/cully\" setup --mcp-url URL"
  fi
fi

# Summarize what Cully offers so a new user knows where to start.
feature_row() { printf '  | %-17s | %-37s | %-52s |\n' "$1" "$2" "$3"; }
feature_rule() { printf '  +-%s-+-%s-+-%s-+\n' "-----------------" "-------------------------------------" "----------------------------------------------------"; }
printf '\n'
say 'What you can do with Cully:'
feature_rule
feature_row 'Feature' 'Command' 'What it does'
feature_rule
feature_row 'Agent terminal' 'cully claude | codex | cursor' 'Run an agent with a health bar and advisor panel'
feature_row 'Advisor' 'cully suggestions' 'Warns on loops, unchecked edits; suggests fixes'
feature_row 'Harness audit' 'cully harness' 'Audits rules, guardrails, memory, skills and hooks'
feature_row 'Agent evals' 'cully eval [setup|harness|...]' 'Scores Cully as an agent on planted ground truth'
feature_row 'Rescue' 'cully rescue' 'Diagnoses a stuck session from recorded evidence'
feature_row 'Timeline, replay' 'cully timeline | replay' 'Shows what the agent did: steps, files, checks'
feature_row 'Handoff' 'cully handoff [AGENT]' 'Hands the session to another coding agent'
feature_row 'Impact, savings' 'cully impact | savings' 'Shows what Cully saved and what it cost in tokens'
feature_row 'Memory' 'cully setup' 'Private project memory for your agents over MCP'
feature_row 'Workflow skill' 'cully workflow' 'Starter skill describing your work, for you to edit'
feature_row 'Team workspace' 'cully workspace' 'Shared tasks, handoffs, review and lessons'
feature_row 'Update' 'cully update [--check]' 'Updates this machine to the latest release'
feature_rule
say 'Run cully help for every command; docs: https://docs.cully.net'

say "Next: fill in your dev-workflow-rules skill so Cully and your agents understand your work: $HOME/.cully/skills/dev-workflow-rules/SKILL.md (linked into your agents' skills directories; hint comments inside explain each section)."
