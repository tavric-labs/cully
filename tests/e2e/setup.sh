#!/usr/bin/env bash
# Run the documented laptop path on an isolated GitHub runner: install, setup,
# connect Codex, use the MCP tools, and remove the disposable local stack.
set -euo pipefail
: "${RUNNER_TEMP:?setup E2E runs only on an isolated CI runner}"

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
original_gopath="$(go env GOPATH)"
original_gocache="$(go env GOCACHE)"
stack_tag="$(git -C "$repo_root" describe --tags --abbrev=0 --match 'v[0-9]*')"
[[ "$stack_tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "No stable Cully release tag for the setup test" >&2; exit 1; }

e2e_root="$(mktemp -d "$RUNNER_TEMP/cully-e2e.XXXXXX")"
trap 'rm -rf -- "$e2e_root"' EXIT
e2e_home="$e2e_root/home"
candidate="$e2e_root/cully-candidate"
mkdir -p "$e2e_home"
cd "$repo_root"
if [[ "${CULLY_E2E_PUBLISHED_CLI:-false}" != true ]]; then
  go build -ldflags "-X main.version=${stack_tag#v}" -o "$candidate" ./cmd/cully
fi

export HOME="$e2e_home"
export CODEX_HOME="$HOME/.codex"
export CULLY_CONFIG_PATH="$HOME/.cully/config.json"
export GOPATH="$original_gopath"
export GOCACHE="$original_gocache"
export PATH="$CODEX_HOME/bin:$PATH"
export SHELL=/bin/bash
export CULLY_E2E_MCP_URL=http://127.0.0.1:3393/mcp
cli="$HOME/.local/bin/cully"
stack_dir="$HOME/.cully/self-hosted/releases/$stack_tag/deploy/self-hosted"

cleanup() {
  "$cli" uninstall claude >/dev/null 2>&1 || true
  if [[ -f "$stack_dir/compose.yaml" ]]; then
    (
      cd "$stack_dir"
      CULLY_DB_NAME=unused CULLY_DB_USER=unused CULLY_DB_PASSWORD=unused \
      CULLY_DATABASE_URL=unused CULLY_DATA_API_TOKEN=unused \
      CULLY_MEM0_DB_PASSWORD=unused CULLY_MEM0_API_KEY=unused CULLY_MEM0_JWT_SECRET=unused \
        docker compose --env-file "$HOME/.cully/self-hosted/config/.env" -f compose.yaml \
          --profile oauth --profile mcp-auth down --volumes --remove-orphans
    ) || true
  fi
  rm -rf -- "$e2e_root"
}
trap cleanup EXIT

cd "$HOME"
sh "$repo_root/install.sh" --agent codex
test -x "$cli"
if [[ "${CULLY_E2E_PUBLISHED_CLI:-false}" == true ]]; then
  test "$("$cli" version)" = "cully ${stack_tag#v}"
else
  # Older published installers start an advisor. Stop it before replacing the
  # executable; the isolated home has no user-owned Claude settings.
  "$cli" uninstall claude
  install -m 0755 "$candidate" "$cli"
  # Candidate tests must exercise this checkout's CLI AND stack script. The
  # published mode below still downloads the matching public release archive.
  mkdir -p "$HOME/.cully/self-hosted/releases/$stack_tag"
  git -C "$repo_root" archive HEAD | tar -x -C "$HOME/.cully/self-hosted/releases/$stack_tag"
fi

# This must work from a clean home without running --prepare first. The CLI
# downloads the matching public release archive and runs its setup script.
project_dir="$e2e_root/project"
mkdir -p "$project_dir"
cd "$project_dir"
"$cli" setup --all
curl --fail --retry 12 --retry-delay 2 --retry-all-errors http://127.0.0.1:3393/healthz
test -f "$CULLY_CONFIG_PATH"
test -f "$CODEX_HOME/skills/cully/SKILL.md"
grep -Fq '[mcp_servers.cully]' "$CODEX_HOME/config.toml"
grep -Fq 'http://127.0.0.1:3393/mcp' "$CODEX_HOME/config.toml"
"$cli" status | tee "$e2e_root/status"
grep -Fq 'cully advisor daemon running' "$e2e_root/status"
test -f "$CODEX_HOME/AGENTS.md"
test ! -e "$project_dir/AGENTS.md"
test ! -e "$project_dir/.cully"
test ! -e "$project_dir/.cursor"
test ! -f "$stack_dir/AGENTS.md"
"$cli" suggestions

cd "$repo_root"
if [[ "${CULLY_E2E_PUBLISHED_CLI:-false}" == true ]]; then
  # Releases before `cully eval` still get the retrieval eval through Go.
  go test ./tests/e2e -run '^(TestSolo|TestRetrievalEval)$' -race -count=1 -v
else
  go test ./tests/e2e -run '^TestSolo$' -race -count=1 -v
  # All four eval suites through the CLI under test, memory against this stack.
  "$cli" eval --mcp-url "$CULLY_E2E_MCP_URL"
fi

cd "$HOME"
"$cli" setup --all --prepare
test -f "$HOME/.cully/self-hosted/config/.env"

# Finish the same flow with the supported uninstall command.
"$cli" uninstall --purge-data
test ! -e "$cli"
test ! -e "$CULLY_CONFIG_PATH"
test ! -e "$HOME/.cully/self-hosted"
test ! -e "$CODEX_HOME/skills/cully/SKILL.md"
test ! -e "$CODEX_HOME/AGENTS.md"
if [[ -f "$CODEX_HOME/config.toml" ]]; then
  ! grep -Fq '[mcp_servers.cully]' "$CODEX_HOME/config.toml"
fi
test -z "$(docker ps -q --filter label=com.docker.compose.project=self-hosted)"
test -z "$(docker volume ls -q --filter label=com.docker.compose.project=self-hosted)"
