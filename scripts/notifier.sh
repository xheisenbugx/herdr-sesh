#!/bin/sh
# Makes "Herdr Notifier.app" in ~/Applications: a copy of terminal-notifier with Herdr's icon
# and its own name, so herdr-sesh's macOS notifications show as Herdr's (not Script Editor's)
# and clicking one takes you to the agent. macOS takes a notification's icon and name from
# the app that sends it, so a renamed copy is the only way to get them.
#
#   sh scripts/notifier.sh            make it (again: after updating Herdr or terminal-notifier)
#   sh scripts/notifier.sh --quiet    make it without sending a test notification
#   sh scripts/notifier.sh --remove   delete it
#
# install.sh runs it (quietly) when terminal-notifier is installed.
# Needs terminal-notifier (brew install terminal-notifier). Without this app sesh-toast uses
# plain terminal-notifier, and without that osascript.
set -eu

DEST="$HOME/Applications/Herdr Notifier.app"
ID="dev.herdr.sesh-notifier"
say() { printf '\033[1;35m›\033[0m %s\n' "$*"; }
die() { printf '\033[1;31m✗\033[0m %s\n' "$*" >&2; exit 1; }

quiet=0
case "${1:-}" in
  --quiet) quiet=1 ;;
  --remove) rm -rf "$DEST"; say "removed $DEST"; exit 0 ;;
  "") ;;
  -h|--help) sed -n '2,14p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
  *) die "unknown option: $1 (try --help)" ;;
esac
[ "$(uname)" = Darwin ] || die "This is for macOS."

tn=$(command -v terminal-notifier) || die "terminal-notifier isn't installed: brew install terminal-notifier"
# The real app behind brew's wrapper script (or a symlink).
src=""
for c in "$(dirname "$(readlink -f "$tn")")/../terminal-notifier.app" "$(brew --prefix terminal-notifier 2>/dev/null)/terminal-notifier.app"; do
  [ -d "$c/Contents/MacOS" ] && { src=$(cd "$c" && pwd); break; }
done
[ -n "$src" ] || src=$(sed -n 's/.*"\(\/[^"]*terminal-notifier\.app\)\/.*/\1/p' "$tn" | head -1)
[ -n "$src" ] && [ -d "$src" ] || die "Couldn't find terminal-notifier.app behind $tn."

herdr=""
for c in /Applications/Herdr.app "$HOME/Applications/Herdr.app"; do [ -d "$c" ] && { herdr=$c; break; }; done
[ -n "$herdr" ] || die "Couldn't find Herdr.app in /Applications or ~/Applications."
icon=$(ls "$herdr"/Contents/Resources/*.icns 2>/dev/null | head -1)
[ -n "$icon" ] || die "Herdr.app has no icon to borrow."

mkdir -p "$HOME/Applications"
rm -rf "$DEST"
cp -R "$src" "$DEST"
rm -f "$DEST"/Contents/Resources/*.icns
cp "$icon" "$DEST/Contents/Resources/Herdr.icns"
P="$DEST/Contents/Info.plist"
plutil -replace CFBundleIdentifier -string "$ID" "$P"
plutil -replace CFBundleName -string "Herdr" "$P"
plutil -replace CFBundleDisplayName -string "Herdr" "$P" 2>/dev/null || plutil -insert CFBundleDisplayName -string "Herdr" "$P"
plutil -replace CFBundleIconFile -string "Herdr" "$P"
xattr -cr "$DEST" 2>/dev/null || true
codesign --force --deep --sign - "$DEST" >/dev/null 2>&1 || die "Couldn't sign $DEST."
# Tell Launch Services about it, so the icon shows at once.
/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister -f "$DEST" 2>/dev/null || true

say "made $DEST (macOS notifications now show as Herdr's)"
[ "$quiet" = 1 ] && exit 0
"$DEST/Contents/MacOS/terminal-notifier" -title "Herdr" -message "Notifications from herdr-sesh will look like this." -activate dev.herdr.app >/dev/null 2>&1 || true
say "sent a test notification: macOS may first ask you to allow notifications from Herdr"
