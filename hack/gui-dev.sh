#!/usr/bin/env bash
# Runs `wails dev` without destroying the packaged app.
#
# wails dev builds its debug binary into gui/build/bin/forgeai.app and, when it
# exits, deletes only Contents/MacOS/forgeai. The bundle directory is left
# behind without an executable, so the next launch reports "forgeai is damaged
# or incomplete" until it is rebuilt. Move the packaged bundle out of the way
# for the duration of the dev session and put it back afterwards, so `make gui`
# output and anything installed from it keep working.
#
#   hack/gui-dev.sh [wails dev flags]
set -euo pipefail

# A go.work in the main checkout (untracked, `use . ./gui`) is picked up from
# worktrees nested under .claude/worktrees and breaks wails' binding generation
# ("main module does not contain package ..."). The GUI is a single module, so
# the workspace is never needed here.
export GOWORK=off

root="$(cd "$(dirname "$0")/.." && pwd)"
bin="$root/gui/build/bin"
app="$bin/forgeai.app"
# Kept outside bin/ so a dev build's `-clean` cannot remove it.
stash="$root/gui/build/forgeai.dev-stash"

# Recover from a dev session that was killed outright (SIGKILL leaves the
# packaged bundle in the stash).
recover() {
  if [[ -d "$stash" ]]; then
    if [[ -d "$app" ]]; then
      rm -rf "$stash"      # a later `make gui` superseded the stashed bundle
    else
      mv "$stash" "$app"
    fi
  fi
}

restore() {
  # Whatever wails dev leaves behind is a bundle without an executable; the
  # stashed one is the packaged build.
  rm -rf "$app"
  recover
}
trap restore EXIT

recover
if [[ -d "$app" ]]; then
  mv "$app" "$stash"
fi

cd "$root/gui"
wails dev "$@" &
dev_pid=$!

# `wails dev` handles SIGINT itself, so a signal sent to this script alone must
# be forwarded for the app to be killed and the dev session to shut down.
forward() { kill -INT "$dev_pid" 2>/dev/null || true; }
trap forward INT TERM

wait "$dev_pid"
