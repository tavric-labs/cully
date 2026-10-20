#!/bin/sh
# Move this VM's Cully deployment controllers from the old ghcr.io/mcp-runtime
# image namespace to ghcr.io/tavric-labs. Run as root on the VM from a checkout
# of main (or a copy of deploy/):
#
#   sh deploy/migrate-registry-namespace.sh --dry-run   # report only
#   sh deploy/migrate-registry-namespace.sh             # migrate
#
# The controllers keep only revision SHAs in their state files, so state is not
# rewritten. The script snapshots what it replaces, retags locally cached images
# so the running containers and rollback targets resolve under the new names
# without a registry pull, installs the updated controllers and Compose files,
# and leaves running containers, state, .env files, SSH keys and Caddy routes
# alone. The next deploy replaces containers through the new controller.
set -eu

OLD=ghcr.io/mcp-runtime/
NEW=ghcr.io/tavric-labs/
OPT=${CULLY_OPT_DIR:-/opt/cully-web}
PERSONAL=${CULLY_PERSONAL_DIR:-/opt/cully-personal-data}
LIBEXEC=${CULLY_LIBEXEC_DIR:-/usr/local/libexec}
BACKUP_DIR=${CULLY_BACKUP_DIR:-/root}
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)

dry=0
case "${1:-}" in
  '') ;;
  --dry-run) dry=1 ;;
  *) echo 'Usage: migrate-registry-namespace.sh [--dry-run]' >&2; exit 1 ;;
esac
if [ -z "${CULLY_MIGRATE_TEST:-}" ]; then
  test "$(id -u)" = 0 || { echo 'Run as root.' >&2; exit 1; }
fi

# Each target is: source file in this checkout, installed destination, mode.
targets="
$script_dir/website_controller.py|$LIBEXEC/cully-web-deploy|755
$script_dir/compose.website.yaml|$OPT/compose.yaml|644
$script_dir/compose.docs.yaml|$OPT/compose.docs.yaml|644
"
if [ -d "$PERSONAL" ]; then
  targets="$targets
$script_dir/personal_data_controller.py|$LIBEXEC/cully-personal-data-deploy|755
$script_dir/compose.personal-data.yaml|$PERSONAL/compose.yaml|644
"
fi

for entry in $targets; do
  src=${entry%%|*}
  test -f "$src" || { echo "Missing $src; run from a checkout of main." >&2; exit 1; }
  grep -q "$NEW" "$src" || { echo "$src does not use $NEW; update the checkout first." >&2; exit 1; }
  if grep -q "$OLD" "$src"; then echo "$src still references $OLD" >&2; exit 1; fi
done

echo "Installed files that still reference $OLD:"
pending=0
for entry in $targets; do
  dest=${entry#*|}; dest=${dest%|*}
  if [ -f "$dest" ] && grep -q "$OLD" "$dest"; then
    echo "  $dest"
    pending=$((pending + 1))
  fi
done
if [ "$pending" = 0 ]; then
  echo '  none: already migrated.'
  exit 0
fi

images=''
if command -v docker >/dev/null 2>&1; then
  images=$(docker image ls --format '{{.Repository}}:{{.Tag}}' | grep "^${OLD}cully-" || true)
fi
echo 'Cached images to retag under the new namespace:'
if [ -n "$images" ]; then echo "$images" | sed 's/^/  /'; else echo '  none'; fi

if [ "$dry" = 1 ]; then
  echo 'Dry run: nothing changed.'
  exit 0
fi

stamp=$(date +%Y%m%d%H%M%S)
backup="$BACKUP_DIR/cully-pre-tavric-$stamp.tgz"
snapshot=''
for entry in $targets; do
  dest=${entry#*|}; dest=${dest%|*}
  test -f "$dest" && snapshot="$snapshot $dest"
done
for state in "$OPT/state.json" "$OPT/docs-state.json" "$PERSONAL/state.json"; do
  test -f "$state" && snapshot="$snapshot $state"
done
# shellcheck disable=SC2086
tar -czf "$backup" $snapshot
chmod 600 "$backup"
echo "Snapshot: $backup"

for image in $images; do
  docker tag "$image" "$NEW${image#"$OLD"}"
done

for entry in $targets; do
  src=${entry%%|*}
  rest=${entry#*|}
  dest=${rest%|*}
  mode=${rest#*|}
  install -m "$mode" "$src" "$dest"
done

python3 -c 'import ast, sys; ast.parse(open(sys.argv[1]).read())' "$LIBEXEC/cully-web-deploy"
for entry in $targets; do
  dest=${entry#*|}; dest=${dest%|*}
  if grep -q "$OLD" "$dest"; then echo "Still references $OLD: $dest" >&2; exit 1; fi
done
echo "Migrated to $NEW. To undo: tar -xzf $backup -C /"
echo 'Next: re-run the failed Docs and Website deploy workflows from GitHub Actions.'
