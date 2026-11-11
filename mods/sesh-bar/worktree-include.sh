#!/bin/sh
# .worktreeinclude for Herdr's worktrees: run in a folder of a git worktree made in the
# last 10 minutes, it copies what the main checkout's .worktreeinclude lists (paths and globs from
# the repo root, "#" comments, "!" lines skipped, nothing outside the repo) into it. Nothing is
# overwritten. Prints "@<worktree root>" first (the caller remembers it), then "copied <path>" lines.
top=$(git rev-parse --show-toplevel 2>/dev/null) || exit 0
# The repo's shared git dir, as a real path (git before 2.31 can't print it absolute).
common=$(git rev-parse --git-common-dir 2>/dev/null) && common=$(cd "$common" && pwd -P) || exit 0
top=$(cd "$top" && pwd -P) || exit 0
main=$(dirname "$common")
echo "@$top"
[ "$main" = "$top" ] && exit 0
[ -f "$top/.git" ] || exit 0
[ -n "$(find "$top/.git" -mmin -10 2>/dev/null)" ] || exit 0
[ -f "$main/.worktreeinclude" ] || exit 0
cd "$main" || exit 0
while IFS= read -r line || [ -n "$line" ]; do
  line=$(printf '%s' "$line" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
  case "$line" in ""|\#*|!*) continue ;; esac
  line=${line#/}; line=${line%/}
  case "$line" in ..*|*/..*) continue ;; esac
  # Globs expand, but an entry isn't split at its spaces ("config/my secrets.env").
  old_ifs=$IFS; IFS=
  set -- $line
  IFS=$old_ifs
  for m in "$@"; do
    [ -e "./$m" ] || continue
    mkdir -p "$top/$(dirname "./$m")"
    # "./": a name starting with "-" is a file, not an option.
    if [ -d "./$m" ]; then mkdir -p "$top/$m" && cp -Rn "./$m/." "$top/$m/" 2>/dev/null || continue
    elif [ ! -e "$top/$m" ]; then cp -p "./$m" "$top/$m" 2>/dev/null || continue
    else continue
    fi
    echo "copied $m"
  done
done < .worktreeinclude
