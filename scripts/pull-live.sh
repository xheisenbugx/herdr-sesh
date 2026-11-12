#!/bin/sh
# Copies your live herdr-sesh files back into this repo: the reverse of install.sh, for
# working on the layout in ~/.config/herdr-app (Herdr reloads it as you save) and then
# shipping the result.
#
#   sh scripts/pull-live.sh            copy the live files over the repo's
#   sh scripts/pull-live.sh --dry-run  only list what would change
#
# It copies the repo's mod folders (files added or removed there too, not Herdr's own
# jsconfig.json, *.d.ts, *.error or hidden files), the layout, its preview and theme, and
# the keys.json sections this repo ships. It lists each file: + new, ~ changed, - removed.
# Herdr's folder is ~/.config/herdr-app, or $HERDR_APP_HOME.
set -eu

APP="${HERDR_APP_HOME:-$HOME/.config/herdr-app}"
REPO=$(cd "$(dirname "$0")/.." && pwd)
LAYOUT="herdr-sesh"
dry=0
case "${1:-}" in
  --dry-run|-n) dry=1 ;;
  "") ;;
  -h|--help) sed -n '2,12p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
  *) echo "unknown option: $1 (try --help)" >&2; exit 2 ;;
esac

say() { printf '\033[1;35m›\033[0m %s\n' "$*"; }
die() { printf '\033[1;31m✗\033[0m %s\n' "$*" >&2; exit 1; }
[ -d "$APP/mods" ] || die "No Herdr folder at $APP."
command -v python3 >/dev/null 2>&1 || die "python3 is needed to copy the keys."

# Herdr's own files in a mod folder, which don't belong in the repo.
skip() { case "$(basename "$1")" in .*|jsconfig.json|*.d.ts|*.error) return 0 ;; esac; case "$1" in */node_modules/*) return 0 ;; esac; return 1; }
changes=""
note() { changes="$changes$1
"; }
# Copies one file when it differs (or is new).
pull() {
  if [ ! -f "$REPO/$1" ]; then note "+ $1"; elif ! cmp -s "$APP/$1" "$REPO/$1"; then note "~ $1"; else return 0; fi
  [ "$dry" = 1 ] || { mkdir -p "$(dirname "$REPO/$1")"; cp "$APP/$1" "$REPO/$1"; }
}

for m in "$REPO"/mods/*/; do
  name=$(basename "$m")
  [ -d "$APP/mods/$name" ] || { say "skipped mods/$name: not in $APP/mods"; continue; }
  for f in $(cd "$APP" && find "mods/$name" -type f | sort); do skip "$f" || pull "$f"; done
  # Gone from the live folder: gone from the repo too.
  for f in $(cd "$REPO" && find "mods/$name" -type f | sort); do
    skip "$f" && continue
    [ -f "$APP/$f" ] && continue
    note "- $f"; [ "$dry" = 1 ] || rm -f "$REPO/$f"
  done
done
for f in "layouts/$LAYOUT.json" "layouts/$LAYOUT.svg" "themes/$LAYOUT.json"; do
  [ -f "$APP/$f" ] && pull "$f"
done

# The repo's keys.json sections, as they are in your keys.json now.
keys=$(python3 - "$APP/keys.json" "$REPO/keys.json" "$dry" <<'PY'
import json, sys
live_path, repo_path, dry = sys.argv[1], sys.argv[2], sys.argv[3] == "1"
live = json.load(open(live_path))
repo = json.load(open(repo_path))
new = {sec: live.get(sec, keys) for sec, keys in repo.items()}
if new != repo:
    for sec in repo:
        a, b = repo[sec], new[sec]
        for k in sorted(set(a) | set(b)):
            if a.get(k) != b.get(k):
                print(f"keys.json [{sec}] {k}: {a.get(k)} -> {b.get(k)}")
    if not dry:
        with open(repo_path, "w") as f:
            json.dump(new, f, indent=2, ensure_ascii=False)
            f.write("\n")
PY
)
[ -n "$keys" ] && note "$keys"

# Mods of the layout you started live that this repo doesn't have yet.
for d in "$APP"/mods/sesh-*/; do
  name=$(basename "$d")
  [ -d "$REPO/mods/$name" ] || say "new live mod not in the repo: mods/$name (copy it in by hand if it belongs)"
done

if [ -z "$changes" ]; then say "the repo already matches $APP"; exit 0; fi
printf '%s' "$changes"
if [ "$dry" = 1 ]; then say "dry run: nothing copied"; exit 0; fi
say "copied into $REPO"
sh "$REPO/mods/sesh-sidebar/sync-lib.sh" --check || die "lib.js copies differ: run mods/sesh-sidebar/sync-lib.sh in $APP, then this again."
