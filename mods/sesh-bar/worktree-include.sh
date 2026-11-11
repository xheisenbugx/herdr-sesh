#!/bin/sh
# ksesh's .worktreeinclude for Herdr's worktrees: run in a folder of a git worktree made in the
# last 10 minutes, it copies what the main checkout's .worktreeinclude lists (paths and globs from
# the repo root, "#" comments, "!" lines skipped, nothing outside the repo) into it. Nothing is
# overwritten. Prints "@<worktree root>" first (the caller remembers it), then "copied <path>" lines.
top=$(git rev-parse --show-toplevel 2>/dev/null) || exit 0
common=$(git rev-parse --path-format=absolute --git-common-dir 2>/dev/null) || exit 0
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
  for m in $line; do
    [ -e "$m" ] || continue
    mkdir -p "$top/$(dirname "$m")"
    if [ -d "$m" ]; then mkdir -p "$top/$m" && cp -Rn "$m/." "$top/$m/" 2>/dev/null
    elif [ ! -e "$top/$m" ]; then cp -p "$m" "$top/$m" 2>/dev/null
    else continue
    fi
    echo "copied $m"
  done
done < .worktreeinclude
