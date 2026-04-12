#!/usr/bin/env bash
# Screenshot & video automation for slackers TUI.
# Usage: Launch slackers in another terminal, then run this script.
#        Click the slackers window when prompted, press Enter, and
#        don't touch anything until it finishes.
#
#   bash scripts/take-screenshots.sh            # screenshots only
#   bash scripts/take-screenshots.sh --video    # also record a demo video

PROJECT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
OUT="$PROJECT_DIR/screenshots"
DELAY=2
RECORD_VIDEO=false
FFMPEG_PID=""

if [[ "${1:-}" == "--video" ]]; then
  RECORD_VIDEO=true
  if ! command -v ffmpeg &>/dev/null; then
    echo "ERROR: ffmpeg not found. Install it for video recording."
    exit 1
  fi
fi

mkdir -p "$OUT"

# ── Ask user to focus the slackers window ──────────────────────
echo ""
echo "=== slackers screenshot automation ==="
echo ""
echo "1. Make sure slackers is running in another terminal."
echo "2. Click the slackers window to focus it."
echo "3. Come back here and press Enter."
echo ""
echo ">>> Press Enter when the slackers window is focused..."
read -r

# Grab whatever window is currently active.
WID=$(xdotool getactivewindow 2>/dev/null || echo "")
if [ -z "$WID" ]; then
  echo "ERROR: Could not detect active window."
  exit 1
fi
echo "Target window: $WID ($(xdotool getwindowname "$WID" 2>/dev/null || echo "unknown"))"
echo ""

# ── Helpers ─────────────────────────────────────────────────────
send_key() {
  xdotool key "$@"
  sleep "$DELAY"
}

send_text() {
  xdotool type --clearmodifiers --delay 80 "$@"
  sleep "$DELAY"
}

capture() {
  local file="$OUT/$1"
  echo "  capture: $1"
  spectacle -a -b -n -o "$file" 2>/dev/null || import -window "$WID" "$file" 2>/dev/null || true
  if [ -f "$file" ]; then
    echo "    saved ($(stat -c%s "$file") bytes)"
  else
    echo "    FAILED"
  fi
  sleep 0.5
}

reset() {
  for _ in 1 2 3 4 5; do xdotool key Escape; sleep 0.3; done
  sleep 1
}

# ── Capture ─────────────────────────────────────────────────────
# ── Start video recording if requested ──────────────────────────
if $RECORD_VIDEO; then
  VIDEO_FILE="$OUT/demo.mp4"
  echo "Recording video to $VIDEO_FILE ..."
  # Get window geometry for ffmpeg region capture.
  eval $(xdotool getwindowgeometry --shell "$WID" 2>/dev/null) || true
  # X/Y/WIDTH/HEIGHT are set by getwindowgeometry --shell
  if [ -n "${WIDTH:-}" ] && [ -n "${HEIGHT:-}" ]; then
    ffmpeg -y -video_size "${WIDTH}x${HEIGHT}" -framerate 15 \
      -f x11grab -i "${DISPLAY:-:0}.0+${X:-0},${Y:-0}" \
      -c:v libx264 -preset ultrafast -crf 23 \
      "$VIDEO_FILE" </dev/null &>/dev/null &
    FFMPEG_PID=$!
    echo "ffmpeg PID: $FFMPEG_PID"
  else
    echo "WARNING: Could not get window geometry, recording full screen"
    ffmpeg -y -framerate 15 \
      -f x11grab -i "${DISPLAY:-:0}.0" \
      -c:v libx264 -preset ultrafast -crf 23 \
      "$VIDEO_FILE" </dev/null &>/dev/null &
    FFMPEG_PID=$!
  fi
  sleep 1
fi

echo "Starting captures... don't touch anything!"
echo ""

echo "=== Main Views ==="
echo "[1/28] Main chat view";           capture "01-main-chat.png"
echo "[2/28] Full-width mode";          send_key ctrl+w; capture "02-full-width.png"; send_key ctrl+w; sleep 1
echo "[3/28] Message select mode";      send_key ctrl+j; capture "03-message-select.png"; send_key Escape; sleep 1
echo "[4/28] Input edit mode"
send_key ctrl+backslash; sleep 1
send_text "Hello from slackers!"; sleep 0.3
xdotool key Return; sleep 0.3
send_text "This is a multi-line"; sleep 0.3
xdotool key Return; sleep 0.3
send_text "input demo."; sleep 1
capture "04-input-edit-mode.png"
# Clear the text and toggle back
xdotool key ctrl+a; sleep 0.2; xdotool key BackSpace; sleep 0.5
send_key ctrl+backslash; sleep 1

echo ""
echo "=== Information ==="
echo "[5/28] Help";                     send_key ctrl+h; capture "05-help.png"; send_key Escape; sleep 1
echo "[6/28] Notifications";            send_key alt+n; capture "06-notifications.png"; send_key Escape; sleep 1

echo ""
echo "=== Settings & Config ==="
echo "[7/28] Settings";                 send_key ctrl+s; capture "07-settings.png"; send_key Escape; sleep 1
echo "[8/28] Shortcuts editor";         send_key alt+k; capture "08-shortcuts-editor.png"; send_key Escape; sleep 1

echo "[9/28] Theme picker"
send_key ctrl+s; sleep 1; send_key Down; send_key Return; sleep 1
capture "09-theme-picker.png"

echo "[10/28] Theme editor"
send_key e; sleep 1; capture "10-theme-editor.png"

echo "[11/28] Theme color picker"
send_key Down; send_key Return; sleep 1; capture "11-theme-color-picker.png"
reset

echo ""
echo "=== Search & Navigation ==="
echo "[12/28] Channel search";          send_key ctrl+k; capture "12-channel-search.png"; send_key Escape; sleep 1
echo "[13/28] Message search";          send_key ctrl+f; capture "13-message-search.png"; send_key Escape; sleep 1
echo "[14/28] Command list";            send_key alt+c; capture "14-command-list.png"; send_key Escape; sleep 1
echo "[15/28] Hidden channels";         send_key ctrl+g; capture "15-hidden-channels.png"; send_key Escape; sleep 1

echo ""
echo "=== Friends & P2P ==="
echo "[16/28] Friend request";          send_key ctrl+b; capture "16-friend-request.png"; send_key Escape; sleep 1
echo "[17/28] Away status";             send_key alt+a; capture "17-away-status.png"; send_key Escape; sleep 1

echo ""
echo "=== Files ==="
echo "[18/28] File browser";            send_key ctrl+u; capture "18-file-browser.png"; send_key Escape; sleep 1
echo "[19/28] Files list";              send_key ctrl+l; capture "19-files-list.png"; send_key Escape; sleep 1
echo "[20/28] Downloads";               send_key alt+d; capture "20-downloads.png"; send_key Escape; sleep 1

echo ""
echo "=== Messages ==="
echo "[21/28] Message options"
send_key ctrl+j; sleep 1; send_key Return; sleep 1; capture "21-message-options.png"
send_key Escape; sleep 0.5; send_key Escape; sleep 1

echo "[22/28] Emoji picker";            send_key ctrl+e; capture "22-emoji-picker.png"; send_key Escape; sleep 1

echo "[23/28] Sidebar options"
send_key shift+Tab; sleep 0.5; send_key Return; sleep 1; capture "23-sidebar-options.png"
send_key Escape; sleep 1; send_key Tab; sleep 1

echo "[24/28] Rename group";            send_key ctrl+a; capture "24-rename-group.png"; send_key Escape; sleep 1

echo ""
echo "=== Extras ==="
echo "[25/28] Snake"
send_text "/games snake"; send_key Return; sleep 2
capture "25-game-snake.png"; send_key Escape; sleep 1; reset

echo "[26/28] Tetris"
send_text "/games tetris"; send_key Return; sleep 2
capture "26-game-tetris.png"; send_key Escape; sleep 1; reset

echo "[27/28] About"
send_key ctrl+s; sleep 1; send_key End; sleep 0.5; send_key Return; sleep 1
capture "27-about.png"; send_key Escape; sleep 1; send_key Escape; sleep 1

echo "[28/28] Whitelist"
# Whitelist is in the P2P section of settings. Use the search filter.
send_key ctrl+s; sleep 1
send_key slash; sleep 0.5
send_text "whitelist"; sleep 1
send_key Down; sleep 0.3  # exit filter, land on Whitelist row
send_key Return; sleep 1
capture "28-whitelist.png"; reset

# ── Stop video recording ────────────────────────────────────────
if $RECORD_VIDEO && [ -n "$FFMPEG_PID" ]; then
  echo ""
  echo "Stopping video recording..."
  kill -INT "$FFMPEG_PID" 2>/dev/null || true
  wait "$FFMPEG_PID" 2>/dev/null || true
  if [ -f "$OUT/demo.mp4" ]; then
    echo "Video saved: $OUT/demo.mp4 ($(stat -c%s "$OUT/demo.mp4" | numfmt --to=iec 2>/dev/null || echo "?") )"
  fi
fi

# ── Index ───────────────────────────────────────────────────────
echo ""
echo "=== Generating index.json ==="
python3 -c "
import json, os, datetime
out = '$OUT'
entries = []
for f in sorted(os.listdir(out)):
    if not f.endswith('.png'): continue
    path = os.path.join(out, f)
    name = f.replace('.png','')
    if len(name) > 3 and name[2] == '-': name = name[3:]
    entries.append({'name': name.replace('-',' '), 'filename': f, 'size_bytes': os.path.getsize(path)})
json.dump({'project':'slackers','generated':datetime.datetime.now().isoformat(),'screenshots':entries},
          open(os.path.join(out,'index.json'),'w'), indent=2)
print(f'Wrote {len(entries)} entries to index.json')
" || echo "(index generation failed)"

# ── Summary ─────────────────────────────────────────────────────
echo ""
echo "=== Summary ==="
COUNT=0
for f in "$OUT"/*.png; do
  [ -f "$f" ] || continue
  COUNT=$((COUNT + 1))
  printf "  %-30s %s\n" "$(basename "$f")" "$(stat -c%s "$f" | numfmt --to=iec 2>/dev/null || stat -c%s "$f")B"
done
echo ""
echo "Total: $COUNT screenshots in $OUT/"
echo "Done!"
