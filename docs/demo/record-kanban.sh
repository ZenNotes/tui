#!/bin/bash
# Records the kanban board demo the way record-demo.sh records the tour: the TUI runs in a
# tmux session, asciinema captures the terminal byte stream of a client
# attached to it, agg renders that to a GIF with a real terminal font, and
# ffmpeg encodes the mp4. No macOS permissions are involved.
#
# needs: tmux, asciinema, agg, ffmpeg (brew install tmux asciinema agg ffmpeg)
# The attached tmux client runs with a UTF-8 locale and -u, otherwise tmux
# replaces every non-ASCII glyph with an underscore in the recording.
# usage: docs/demo/record-demo.sh [path-to-zn-binary]
set -u
HERE="$(cd "$(dirname "$0")" && pwd)"
REPO="$(cd "$HERE/../.." && pwd)"
ZN="${1:-$REPO/zn}"
OUT_DIR="$REPO/docs/media"
WORK="$(mktemp -d /tmp/zn-kanban.XXXXXX)"
SOCK="znkanban"
COLS=150; ROWS=40
FONT="${DEMO_FONT:-SF Mono,Menlo,Apple Symbols}"
FONT_SIZE="${DEMO_FONT_SIZE:-16}"
# Gruvbox Dark Hard: bg, fg, then the 16 ANSI colors.
THEME="1d2021,ebdbb2,282828,cc241d,98971a,d79921,458588,b16286,689d6a,a89984,928374,fb4934,b8bb26,fabd2f,83a598,d3869b,8ec07c,ebdbb2"

for tool in tmux asciinema agg ffmpeg; do
  command -v "$tool" >/dev/null || { echo "missing $tool (brew install $tool)" >&2; exit 1; }
done
if [ ! -x "$ZN" ]; then
  echo "building zn into $WORK" >&2
  (cd "$REPO" && go build -o "$WORK/zn" ./cmd/zn) || exit 1
  ZN="$WORK/zn"
fi
mkdir -p "$OUT_DIR"
cp -R "$HERE/kanban-vault" "$WORK/Notes"
mkdir -p "$WORK/config" && cp "$HERE/kanban-config/config.toml" "$WORK/config/config.toml"
cleanup() { tmux -L "$SOCK" kill-server 2>/dev/null; rm -rf "$WORK"; }
trap cleanup EXIT

tmux -L "$SOCK" kill-server 2>/dev/null
tmux -L "$SOCK" new-session -d -s demo -n app -x $COLS -y $ROWS \
  "env ZENNOTES_VAULT='$WORK/Notes' ZENNOTES_CONFIG_DIR='$WORK/config' TERM=xterm-256color LANG=en_US.UTF-8 '$ZN' tui; sleep 1" || exit 1
tmux -L "$SOCK" set -g status off
sleep 2
asciinema rec --headless --overwrite --window-size ${COLS}x${ROWS} \
  -c "env TERM=xterm-256color LANG=en_US.UTF-8 LC_ALL=en_US.UTF-8 tmux -u -L $SOCK attach -t demo" "$OUT_DIR/zn-tui-kanban.cast" >"$WORK/asciinema.log" 2>&1 &
REC=$!
sleep 2.5
"$HERE/kanban-drive.sh" -L "$SOCK" -t demo:app
wait "$REC"
[ -s "$OUT_DIR/zn-tui-kanban.cast" ] || { echo "no recording was produced" >&2; cat "$WORK/asciinema.log" >&2; exit 1; }

agg --font-family "$FONT" --font-size "$FONT_SIZE" --line-height 1.35 --theme "$THEME" \
  --fps-cap 30 --no-loop --last-frame-duration 2 "$OUT_DIR/zn-tui-kanban.cast" "$WORK/demo.gif" || exit 1
ffmpeg -v error -y -i "$WORK/demo.gif" -vf "scale=trunc(iw/2)*2:trunc(ih/2)*2" -c:v libx264 -preset slow -crf 18 -pix_fmt yuv420p -movflags +faststart -r 30 "$OUT_DIR/zn-tui-kanban.mp4" || exit 1
ffmpeg -v error -y -i "$WORK/demo.gif" -vf "scale=1280:-2" -c:v libx264 -preset slow -crf 20 -pix_fmt yuv420p -movflags +faststart -r 30 "$OUT_DIR/zn-tui-kanban-1280.mp4" || exit 1
ffmpeg -v error -y -ss 10 -i "$OUT_DIR/zn-tui-kanban.mp4" -frames:v 1 -update 1 "$OUT_DIR/zn-tui-kanban-poster.png"
ls -la "$OUT_DIR"
