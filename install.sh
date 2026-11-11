#!/bin/sh
# Installs (or updates, or removes) the herdr-sesh layout for the Herdr desktop app.
# Herdr's folder is ~/.config/herdr-app, or $HERDR_APP_HOME.
set -eu

usage() {
  cat <<'EOF'
Installs (or updates, or removes) the herdr-sesh layout for the Herdr desktop app.

  sh install.sh               install or update, then switch to the layout
  sh install.sh --no-switch   install or update, stay on your current layout
  sh install.sh --force       also replace files of yours with the same names (each kept as .bak-<time>)
  sh install.sh --uninstall   remove what it installed, and its keys

Or without cloning:
  curl -fsSL https://raw.githubusercontent.com/xheisenbugx/herdr-sesh/main/install.sh | sh
  curl -fsSL https://raw.githubusercontent.com/xheisenbugx/herdr-sesh/main/install.sh | sh -s -- --no-switch

Herdr's folder is ~/.config/herdr-app, or $HERDR_APP_HOME.
EOF
}

REPO="https://github.com/xheisenbugx/herdr-sesh"
APP="${HERDR_APP_HOME:-$HOME/.config/herdr-app}"
MANIFEST="$APP/.herdr-sesh"           # every file it installed, one path per line
KEYSNAP="$APP/.herdr-sesh-keys.json"  # the keys it last installed (to tell your edits apart)
CONFIG="$HOME/.config/herdr-sesh/config.toml"
LAYOUT="herdr-sesh"
STAMP=$(date +%s)

force=0 switch=1 uninstall=0
for arg in "$@"; do
  case "$arg" in
    --force) force=1 ;;
    --no-switch) switch=0 ;;
    --uninstall) uninstall=1 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown option: $arg (try --help)" >&2; exit 2 ;;
  esac
done

say() { printf '\033[1;35m›\033[0m %s\n' "$*"; }
die() { printf '\033[1;31m✗\033[0m %s\n' "$*" >&2; exit 1; }

[ -d "$APP" ] || die "No Herdr folder at $APP. Install Herdr (https://herdr.dev) and open it once, then run this again."
# Installs from before the repo was renamed kept their list under the old name.
if [ -f "$APP/.herdr-xheisenbugx" ] && [ ! -f "$MANIFEST" ]; then mv "$APP/.herdr-xheisenbugx" "$MANIFEST"; fi
command -v python3 >/dev/null 2>&1 || die "python3 is needed to merge keys.json (on macOS: xcode-select --install)."
herdr_app() { command -v herdr-app >/dev/null 2>&1 && herdr-app "$@"; }

# Merges this layout's keys into keys.json, or takes them out, keeping a backup.
# Three-way, key by key, against what it installed last time: a key you changed or removed
# stays as you left it; one you didn't follows the layout; new ones are added. Only the
# sections the layout ships are touched. Prints "updated" when keys.json changed.
keys() {
  python3 - "$APP/keys.json" "$KEYSNAP" "$1" "${2:-}" "$STAMP" <<'PY'
import json, os, shutil, sys
path, snap_path, mode, src, stamp = sys.argv[1:6]
if not os.path.exists(path):
    if mode == "remove":
        sys.exit(0)
    sys.exit("keys.json not found in Herdr's folder: open Herdr once so it writes it, then run this again.")
try:
    with open(path) as f:
        keys = json.load(f)
    assert isinstance(keys, dict)
except Exception as e:
    sys.exit(f"keys.json isn't valid JSON ({e}): fix it (Herdr shows why in keys.error), then run this again.")
snap = {}
if os.path.exists(snap_path):
    with open(snap_path) as f:
        snap = json.load(f)
repo = {}
if mode == "add":
    with open(src) as f:
        repo = json.load(f)
new = json.loads(json.dumps(keys))
for sec in set(repo) | set(snap):
    mine = new.get(sec, {})
    # No snapshot (installed before there was one): what's there is taken as installed.
    last = snap.get(sec, mine if sec not in snap and not snap else {})
    want = repo.get(sec, {})
    out = dict(mine)
    for k in set(last) | set(want):
        if k in last and (k not in mine or mine[k] != last[k]):
            continue  # you changed or removed it: yours stays
        if k in want:
            out[k] = want[k]
        else:
            out.pop(k, None)  # the layout dropped it, and you hadn't changed it
    if out:
        new[sec] = out
    else:
        new.pop(sec, None)
if new != keys:
    shutil.copy2(path, f"{path}.bak-{stamp}")
    with open(path + ".tmp", "w") as f:
        json.dump(new, f, indent=2, ensure_ascii=False)
        f.write("\n")
    os.replace(path + ".tmp", path)
    print("updated")
if mode == "add":
    with open(snap_path + ".tmp", "w") as f:
        json.dump(repo, f, indent=2, ensure_ascii=False)
    os.replace(snap_path + ".tmp", snap_path)
elif os.path.exists(snap_path):
    os.remove(snap_path)
PY
}

# Lists what an older manifest named whole folders as: their files now.
manifest_files() {
  while IFS= read -r item; do
    [ -n "$item" ] || continue
    if [ -d "$APP/$item" ]; then (cd "$APP" && find "$item" -type f); else printf '%s\n' "$item"; fi
  done
}

# Removes now-empty folders a removed file was in (mods/<name>/…), never the top ones.
prune() {
  d=$(dirname "$1")
  while [ "$d" != "." ] && [ "$d" != "mods" ] && [ "$d" != "layouts" ] && [ "$d" != "themes" ]; do
    rmdir "$APP/$d" 2>/dev/null || break
    d=$(dirname "$d")
  done
}

if [ "$uninstall" = 1 ]; then
  [ -f "$MANIFEST" ] || die "Nothing to remove: $MANIFEST is missing."
  if herdr_app status 2>/dev/null | grep -q "\"layout\": \"$LAYOUT\""; then
    herdr_app do layout.switch --name main >/dev/null 2>&1 || true
  fi
  n=0
  manifest_files < "$MANIFEST" > "$MANIFEST.files"
  while IFS= read -r f; do
    if [ -e "$APP/$f" ]; then rm -f "$APP/$f"; n=$((n + 1)); prune "$f"; fi
  done < "$MANIFEST.files"
  rm -f "$MANIFEST" "$MANIFEST.files"
  say "removed $n files it installed"
  left=$(cd "$APP" && find mods -path 'mods/sesh-*' -type f 2>/dev/null | head -5)
  [ -n "$left" ] && say "kept files you added in its mod folders: $(echo "$left" | tr '\n' ' ')"
  out=$(keys remove) || exit 1
  [ "$out" = updated ] && say "removed its keys from keys.json (your old one is kept as keys.json.bak-$STAMP)"
  [ -f "$CONFIG" ] && say "kept your settings in $CONFIG (delete it yourself if you want)"
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

# Every file to install, relative to Herdr's folder (a mod's subfolders too).
list="$APP/.herdr-sesh.list"
{
  printf '%s\n' "layouts/$LAYOUT.json" "layouts/$LAYOUT.svg" "themes/$LAYOUT.json"
  (cd "$src" && find mods -type f ! -name '.DS_Store' | sort)
} > "$list"
old="$APP/.herdr-sesh.old"
if [ -f "$MANIFEST" ]; then manifest_files < "$MANIFEST" > "$old"; else : > "$old"; fi

# Never overwrite something of yours with the same name, unless asked (then it's kept aside).
clash=""
while IFS= read -r f; do
  if [ -e "$APP/$f" ] && ! grep -qxF "$f" "$old"; then clash="$clash
  $f"; fi
done < "$list"
if [ -n "$clash" ]; then
  if [ "$force" = 0 ]; then
    rm -f "$list" "$old"
    die "These already exist and weren't installed by this script:$clash
  Rename them, or run again with --force to replace them (each is kept as <name>.bak-$STAMP)."
  fi
  echo "$clash" | while IFS= read -r f; do
    f=$(echo "$f" | sed 's/^ *//'); [ -n "$f" ] && mv "$APP/$f" "$APP/$f.bak-$STAMP" && say "kept yours as $f.bak-$STAMP"
  done
fi

# mod.json first: Herdr docks a new mod without one into the layout you're looking at.
copy() { mkdir -p "$(dirname "$APP/$1")" && cp -p "$src/$1" "$APP/$1"; }
grep '/mod\.json$' "$list" | while IFS= read -r f; do copy "$f"; done
grep -v '/mod\.json$' "$list" | while IFS= read -r f; do copy "$f"; done

# Files an earlier version installed that this one doesn't have any more.
while IFS= read -r f; do
  if [ -n "$f" ] && ! grep -qxF "$f" "$list" && [ -e "$APP/$f" ]; then rm -f "$APP/$f"; prune "$f"; fi
done < "$old"
mv "$list" "$MANIFEST"
rm -f "$old"
say "copied the layout, its theme and $(ls -d "$src"/mods/*/ | wc -l | tr -d ' ') mods into $APP"

out=$(keys add "$src/keys.json") || exit 1
[ "$out" = updated ] && say "merged the layout's keys into keys.json (your old one is kept as keys.json.bak-$STAMP)"

# Its own settings file, from the commented example, only if you have none.
if [ ! -f "$CONFIG" ]; then
  mkdir -p "$(dirname "$CONFIG")"
  cp "$src/config.example.toml" "$CONFIG"
  say "wrote $CONFIG (all commented out: add your sessions there)"
fi

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
