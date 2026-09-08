#!/bin/bash
# Drives the zn tui demo choreography into a tmux target.
# usage: demo-drive.sh [-L socket] [-t target] [--fast]
SOCK=""; TARGET="demo:app"; SPEED=1
while [ $# -gt 0 ]; do
  case "$1" in
    -L) SOCK="-L $2"; shift 2;;
    -t) TARGET="$2"; shift 2;;
    --fast) SPEED=0.25; shift;;
    *) shift;;
  esac
done
tm() { tmux $SOCK "$@"; }
k() { tm send-keys -t "$TARGET" "$@"; }
p() { sleep "$(echo "$1 * $SPEED" | bc -l)"; }
t() { # type text at a human pace
  local s="$1" i ch
  for ((i=0; i<${#s}; i++)); do
    ch="${s:$i:1}"
    if [ "$ch" = " " ]; then k Space; elif [ "$ch" = ";" ]; then k "\;"; else k -l "$ch"; fi
    sleep "$(echo "0.06 * $SPEED" | bc -l)"
  done
}
p 2.5
# Leader hints, then search for a note
k Space; p 2.2
k f; p 0.8; t "launch"; p 1.2; k Enter; p 1.8
# Vim editing: append a task, toggle it, visual delete and undo
k G; p 0.4; k o; p 0.3; t "- [ ] Announce the terminal app due:2026-09-12 !high"; p 0.5; k Escape; p 1.0
k C-l; p 1.0; k C-l; p 0.8
k g; k g; p 0.3; k j; k j; p 0.3; k V; p 0.5; k j; p 0.5; k d; p 1.0; k u; p 1.2
# Split and preview modes
k M-s; p 2.6; k M-p; p 2.2; k M-e; p 0.8
# Hint mode follows a link
k Space; p 0.5; k h; p 1.8; k a; p 1.8
# Tasks: list, toggle, board, calendar
k Space; p 0.5; k x; p 2.0; k j; k j; p 0.4; k x; p 1.4; k v; p 2.4; k v; p 2.4
# Tags
t ":tags"; k Enter; p 2.2
# Panes
k C-w; k v; p 1.0; k C-w; k h; p 0.6; t ":e Roadmap"; k Enter; p 2.0; k C-w; k o; p 0.8
# Quick capture
k Space; p 0.5; k q; p 0.8; t "Demo idea"; k Enter; t "The terminal is a fine place for notes."; p 0.8; k C-s; p 1.6
# Daily note and the manual
k Space; p 0.5; k d; p 1.8
t ":help"; k Enter; p 2.4
t ":qa"; k Enter; p 1.0
