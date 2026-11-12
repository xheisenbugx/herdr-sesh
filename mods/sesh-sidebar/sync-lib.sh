#!/bin/sh
# Copies the shared thread model to every herdr-sesh mod (mods cannot import across folders).
#   sh sync-lib.sh           copy mods/sesh-sidebar/lib.js into the others
#   sh sync-lib.sh --check   copy nothing; fail, naming them, if any copy differs
set -eu
cd "$(dirname "$0")/.."
MODS="sesh-agents sesh-bar sesh-sessions sesh-menu sesh-term-menu"
if [ "${1:-}" = "--check" ]; then
  bad=""
  for m in $MODS; do cmp -s sesh-sidebar/lib.js "$m/lib.js" || bad="$bad $m"; done
  [ -z "$bad" ] && exit 0
  echo "lib.js differs from mods/sesh-sidebar/lib.js in:$bad (run mods/sesh-sidebar/sync-lib.sh)" >&2
  exit 1
fi
for m in $MODS; do cp sesh-sidebar/lib.js "$m/lib.js"; done
