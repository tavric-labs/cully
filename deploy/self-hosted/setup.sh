#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$script_dir"

stage='checking prerequisites'
trap 'result=$?; if [ "$result" -ne 0 ]; then printf "\nCully setup failed during %s (exit %s). Fix the error above and rerun setup.\n" "$stage" "$result" >&2; fi' EXIT
say() { printf '\n==> %s\n' "$1"; }

mode=none
agent=""
endpoint=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --oauth)
      [ "$#" -ge 2 ] || { echo '--oauth requires mcp-auth' >&2; exit 2; }
      mode="$2"; shift 2 ;;
    claude|codex|cursor|all)
      [ -z "$agent" ] || { echo 'Choose one agent.' >&2; exit 2; }
      agent="$1"; shift ;;
    *) echo 'Usage: ./setup.sh [--oauth mcp-auth] [claude|codex|cursor|all]' >&2; exit 2 ;;
  esac
done
case "$mode" in
  none|mcp-auth) ;;
  *) echo 'Choose --oauth mcp-auth.' >&2; exit 2 ;;
esac
say '[1/7] Checking Docker Compose and the Cully CLI'
command -v docker >/dev/null 2>&1 || { echo 'Docker Compose is required for self-hosting.' >&2; exit 1; }
docker compose version >/dev/null 2>&1 || { echo 'Docker Compose is required for self-hosting.' >&2; exit 1; }
docker info >/dev/null 2>&1 || { echo 'Docker daemon is unavailable; start Docker and rerun setup.' >&2; exit 1; }
cli=${CULLY_CLI_BINARY:-cully}
if ! command -v "$cli" >/dev/null 2>&1; then
  if [ -x "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/bin/cully" ]; then
    cli="${CLAUDE_CONFIG_DIR:-$HOME/.claude}/bin/cully"
  elif [ -x "${CODEX_HOME:-$HOME/.codex}/bin/cully" ]; then
    cli="${CODEX_HOME:-$HOME/.codex}/bin/cully"
  elif [ -x "${CURSOR_CONFIG_DIR:-$HOME/.cursor}/bin/cully" ]; then
    cli="${CURSOR_CONFIG_DIR:-$HOME/.cursor}/bin/cully"
  elif [ -x ../../build/cully ]; then
    cli=../../build/cully
  else
    echo 'Install the Cully CLI first, then rerun setup.' >&2
    exit 1
  fi
fi
# Keep the CLI path absolute while configuring user-level integrations.
cli=$(command -v "$cli")
case "$cli" in /*) ;; *) cli="$script_dir/$cli" ;; esac
stage='preparing credentials'
say '[2/7] Generating or reusing private database passwords, API keys and owner ID (values are not printed)'
"$cli" _internal self-hosted-credentials ensure
if [ ! -f .env ]; then
  cp .env.example .env
  chmod 600 .env
fi
exports=$("$cli" _internal self-hosted-credentials export)
eval "$exports"
if [ "$mode" = mcp-auth ]; then
  if [ "${CULLY_CUSTOM_AUTH:-}" != 1 ]; then
    [ -f connectors.json ] || { echo 'Create connectors.json for your identity provider first.' >&2; exit 2; }
  fi
  [ -f .secrets/signing-key.pem ] || { echo 'Create .secrets/signing-key.pem first.' >&2; exit 2; }
fi
if [ "$mode" != none ]; then
  say 'Validating OAuth hostnames and authentication configuration'
  oauth_exports=$("$cli" _internal self-hosted-credentials oauth-env "$mode")
  eval "$oauth_exports"
  endpoint=$CULLY_AUTH_RESOURCE
fi
compose() {
  case "$mode" in
    none) CULLY_MCP_AUTH_MODE=none docker compose --env-file .env -f compose.yaml "$@" ;;
    mcp-auth)
      if [ "${CULLY_CUSTOM_AUTH:-}" = 1 ]; then
        CULLY_MCP_AUTH_MODE=oauth CULLY_MCP_OWNER='' docker compose --env-file .env -f compose.yaml -f compose.custom-auth.yaml --profile oauth "$@"
      else
        CULLY_MCP_AUTH_MODE=oauth CULLY_MCP_OWNER='' docker compose --env-file .env -f compose.yaml --profile oauth --profile mcp-auth "$@"
      fi ;;
  esac
}

say 'Choosing free local ports (MCP prefers 3393; other services use uncommon ports)'
running=""
if [ -n "$(compose ps -q mcp 2>/dev/null)" ]; then running="--running"; fi
"$cli" _internal self-hosted-ports ensure $running
stage='validating Compose'
say '[3/7] Checking Docker Compose configuration'
compose config --quiet
if [ "${CULLY_CUSTOM_AUTH:-}" = 1 ]; then
  bundled_auth=$(CULLY_MCP_AUTH_MODE=oauth CULLY_MCP_OWNER='' docker compose --env-file .env -f compose.yaml --profile mcp-auth ps -q mcp-auth)
  if [ -n "$bundled_auth" ]; then
    echo 'The bundled mcp-auth service is running. Stop it before switching to the custom auth service; preserve its state and signing key.' >&2
    exit 2
  fi
fi
stage='starting databases'
say '[4/7] Pulling images if needed and starting PostgreSQL databases'
compose up -d --wait db mem0-db
stage='migrating the schema'
say '[5/7] Applying the Cully database schema'
compose --profile ops run --rm migrate
say 'The Mem0 image includes the local embedding model; its first build downloads and caches that model'
case "$mode" in
  none)
    stage='building and starting memory services'
    say '[6/7] Building and starting Cully MCP, the private data API and Mem0'
    compose up -d --build --wait data-api mem0 mcp ;;
  mcp-auth)
    stage='building and starting memory services'
    say '[6/7] Building and starting Cully MCP, the private data API, Mem0, MCP Auth and Caddy'
    auth_service=mcp-auth
    [ "${CULLY_CUSTOM_AUTH:-}" != 1 ] || auth_service=mcp-auth-custom
    compose up -d --build --wait data-api mem0 mcp "$auth_service" caddy ;;
esac

if [ "$mode" = none ]; then
  endpoint="http://$(compose port mcp 8080)/mcp"
fi
say 'Local Cully services are ready'
echo "Cully MCP is configured at $endpoint"
stage='configuring agents and verifying MCP'
say '[7/7] Configuring agent skills, hooks and MCP connections; starting the advisor and verifying MCP'
set -- setup --mcp-url "$endpoint"
[ -z "$agent" ] || set -- "$@" --agent "$agent"
[ "$mode" = none ] || set -- "$@" --oauth
"$cli" "$@"
