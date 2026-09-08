#!/bin/bash
# Drives the kanban demo choreography into a tmux target: the board with
# five status columns, cards moving between them, the three other
# groupings, a filter, a new card, and a card finished.
# usage: kanban-drive.sh [-L socket] [-t target] [--fast]
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
p 2.0
# Leader, then straight to the board; hide the sidebar for room.
k Space; p 1.8; k k; p 2.4
k Space; p 0.4; k e; p 2.0
# Walk the columns and cards.
k l; p 0.9; k l; p 0.9; k l; p 0.9; k h; k h; k h; p 0.6; k j; k j; k j; k j; k j; p 1.2
# Move a card right twice: To Do -> In Progress -> In Review, then back once.
k L; p 1.6; k L; p 1.6; k H; p 1.4
# Other groupings: priority, due date, owner, back to status.
k g; p 3.6; k g; p 3.8; k g; p 3.6; k g; p 2.0
# Open the card's note, then return to the board.
k Enter; p 2.4; k Space; p 0.4; k k; p 1.6
# Filter the board by a tag, then clear it.
k f; p 0.6; t "#launch"; k Enter; p 2.6; k F; p 1.2
# A new card lands in To Do; finish one with x.
k n; p 0.6; t "Announce on Discord due:2026-09-09 !med #launch"; k Enter; p 2.2
k x; p 2.2
t ":qa"; k Enter; p 1.0
