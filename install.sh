#!/bin/sh
# Installs (or updates, or removes) the herdr-sesh layout for the Herdr desktop app.
#
#   sh install.sh               install or update, then switch to the layout
#   sh install.sh --no-switch   install or update, stay on your current layout
#   sh install.sh --force       also replace files with the same names that this installer didn't put there
#   sh install.sh --uninstall   remove everything it installed, and its keys
#
# Or without cloning:
#   curl -fsSL https://raw.githubusercontent.com/xheisenbugx/herdr-xheisenbugx/main/install.sh | sh
#
# Herdr's folder is ~/.config/herdr-app, or $HERDR_APP_HOME.
set -eu

REPO="https://github.com/xheisenbugx/herdr-xheisenbugx"
APP="${HERDR_APP_HOME:-$HOME/.config/herdr-app}"
MANIFEST="$APP/.herdr-xheisenbugx"
LAYOUT="herdr-sesh"

force=0 switch=1 uninstall=0
for arg in "$@"; do
  case "$arg" in
    --force) force=1 ;;
    --no-switch) switch=0 ;;
    --uninstall) uninstall=1 ;;
    -h|--help) sed -n '2,13p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "unknown option: $arg (try --help)" >&2; exit 2 ;;
  esac
done

say() { printf '\033[1;35m›\033[0m %s\n' "$*"; }
die() { printf '\033[1;31m✗\033[0m %s\n' "$*" >&2; exit 1; }

[ -d "$APP" ] || die "No Herdr folder at $APP. Install Herdr (https://herdr.dev) and open it once, then run this again."
command -v python3 >/dev/null 2>&1 || die "python3 is needed to merge keys.json (on macOS: xcode-select --install)."
herdr_app() { command -v herdr-app >/dev/null 2>&1 && herdr-app "$@"; }

# Merges (or removes) this layout's sections of keys.json, keeping a backup.
keys() {
  python3 - "$APP/keys.json" "$1" "${2:-}" <<'PY'
import json, os, shutil, sys, time
path, mode, src = sys.argv[1], sys.argv[2], sys.argv[3]
mine = lambda k: k == "layout:herdr-sesh" or k.startswith("mod:sesh-")
if not os.path.exists(path):
    if mode == "remove":
        sys.exit(0)
    sys.exit("keys.json not found: open Herdr once so it writes it, then run this again.")
with open(path) as f:
    keys = json.load(f)
new = {k: v for k, v in keys.items() if not mine(k)}
if mode == "add":
    with open(src) as f:
        new.update(json.load(f))
if new != keys:
    shutil.copy2(path, f"{path}.bak-{int(time.time())}")
    with open(path + ".tmp", "w") as f:
        json.dump(new, f, indent=2, ensure_ascii=False)
        f.write("\n")
    os.replace(path + ".tmp", path)
    print("updated")
PY
}

if [ "$uninstall" = 1 ]; then
  [ -f "$MANIFEST" ] || die "Nothing to remove: $MANIFEST is missing."
  if herdr_app status 2>/dev/null | grep -q "\"layout\": \"$LAYOUT\""; then
    herdr_app do layout.switch --name main >/dev/null 2>&1 || true
  fi
  while IFS= read -r item; do
    [ -n "$item" ] && rm -rf "${APP:?}/$item" && say "removed $item"
  done < "$MANIFEST"
  rm -f "$MANIFEST"
  [ "$(keys remove)" = updated ] && say "removed its keys from keys.json (backup next to it)"
  say "herdr-sesh is gone."
  exit 0
fi

# Where the files are: next to this script, or a fresh clone.
src=""
[ -f "$0" ] && src=$(cd "$(dirname "$0")" && pwd)
if [ -z "$src" ] || [ ! -f "$src/layouts/$LAYOUT.json" ]; then
  command -v git >/dev/null 2>&1 || die "git is needed to download the layout."
  tmp=$(mktemp -d)
  trap 'rm -rf "$tmp"' EXIT
  say "downloading $REPO"
  git clone --quiet --depth 1 "$REPO" "$tmp/repo"
  src="$tmp/repo"
fi

items="layouts/$LAYOUT.json layouts/$LAYOUT.svg themes/$LAYOUT.json"
for m in "$src"/mods/*/; do items="$items mods/$(basename "$m")"; done

# Never overwrite something of yours with the same name, unless asked.
owned() { [ -f "$MANIFEST" ] && grep -qx "$1" "$MANIFEST"; }
clash=""
for item in $items; do
  if [ -e "$APP/$item" ] && ! owned "$item"; then clash="$clash $item"; fi
done
if [ -n "$clash" ] && [ "$force" = 0 ]; then
  die "These already exist and weren't installed by this script:$clash
  Rename them, or run again with --force to replace them."
fi

mkdir -p "$APP/layouts" "$APP/themes" "$APP/mods"
: > "$MANIFEST.tmp"
for item in $items; do
  case "$item" in
    mods/*)
      dest="$APP/$item"
      mkdir -p "$dest"
      # mod.json first: Herdr docks a new mod without one into the layout you're looking at.
      cp -p "$src/$item/mod.json" "$dest/mod.json"
      for f in "$src/$item"/*; do
        [ "$(basename "$f")" = mod.json ] || cp -p "$f" "$dest/"
      done
      ;;
    *) cp -p "$src/$item" "$APP/$item" ;;
  esac
  echo "$item" >> "$MANIFEST.tmp"
done
mv "$MANIFEST.tmp" "$MANIFEST"
say "copied the layout, its theme and $(ls -d "$src"/mods/*/ | wc -l | tr -d ' ') mods into $APP"

[ "$(keys add "$src/keys.json")" = updated ] && say "added the layout's keys to keys.json (your old one is kept as keys.json.bak-*)"

if command -v herdr-app >/dev/null 2>&1; then
  failed=0
  for m in "$src"/mods/*/; do
    herdr-app check "$APP/mods/$(basename "$m")" >/dev/null 2>&1 || { failed=1; echo "  check failed: $(basename "$m")"; herdr-app check "$APP/mods/$(basename "$m")" || true; }
  done
  [ "$failed" = 0 ] && say "every mod passes herdr-app check"
  if [ "$switch" = 1 ]; then
    if herdr-app do layout.switch --name "$LAYOUT" >/dev/null 2>&1; then
      say "switched to $LAYOUT (cmd-shift-l cycles layouts)"
    else
      say "Herdr isn't open: pick \"$LAYOUT\" in Settings → Look, or press cmd-shift-l"
    fi
  fi
else
  say "open Herdr and pick \"$LAYOUT\" in Settings → Look (or press cmd-shift-l)"
fi
say "done. Press cmd-p for sessions, cmd-a for agents, ? in the sidebar for its keys."
