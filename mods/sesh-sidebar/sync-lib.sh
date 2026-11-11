#!/bin/sh
# Copies the shared thread model to every herdr-sesh mod (mods cannot import across folders).
cd "$(dirname "$0")/.." && for m in sesh-agents sesh-bar sesh-sessions sesh-menu; do cp sesh-sidebar/lib.js "$m/lib.js"; done
