#!/bin/sh
# Quick checks that need no Herdr: the lib.js copies match, every mod's JavaScript parses,
# the JSON files are valid and the shell scripts parse. CI and the pre-commit hook run it;
# `herdr-app check ~/.config/herdr-app/mods/<name>` is still the real test of a mod.
set -eu
cd "$(dirname "$0")/.."
fail=0
sh mods/sesh-sidebar/sync-lib.sh --check || fail=1
if command -v node >/dev/null 2>&1; then
  for f in mods/*/*.js; do
    # As an ES module: the imports aren't resolved, only the syntax is read.
    node --input-type=module --check < "$f" 2>/dev/null || { echo "doesn't parse: $f" >&2; node --input-type=module --check < "$f" || true; fail=1; }
  done
else
  echo "node not found: skipped the JavaScript check" >&2
fi
for f in keys.json layouts/*.json themes/*.json mods/*/mod.json; do
  python3 -c 'import json,sys; json.load(open(sys.argv[1]))' "$f" 2>/dev/null || { echo "not valid JSON: $f" >&2; fail=1; }
done
for f in install.sh scripts/*.sh mods/*/*.sh .githooks/*; do
  [ -f "$f" ] || continue
  sh -n "$f" || { echo "doesn't parse: $f" >&2; fail=1; }
done
[ "$fail" = 0 ] && echo "checks passed"
exit "$fail"
