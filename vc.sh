#!/bin/bash

# Configuration
VIBE_DIR="$HOME/Vibe-Core"
VIBE_BIN="$VIBE_DIR/./vibe-core" # Path to your compiled Go binary
MUSIC_DIR="$VIBE_DIR/music"
META_CACHE="/dev/shm/vibe/meta"
MUTE_FILE="/dev/shm/vibe/mute_store"

# 1. AUTO-START ENGINE
if ! pgrep -x "vibe-core" >/dev/null; then
  echo "[!] Engine not running. Starting Vibe-Core... Vibe-Core is at $VIBE_BIN"
  $VIBE_BIN >/dev/null 2>&1 &
  sleep 1 # Give the engine a second to create pipes
fi

# Initialize UI and State
LAST_SONG=""
DURATION=0
TITLE="Loading..."
tput civis
stty -echo
trap "stty echo; tput cnorm; rm -f $META_CACHE; clear; exit" EXIT
clear

while true; do
  # 2. READ ENGINE STATE (Direct Hooks)
  [[ -f "$VIBE_DIR/now_playing" ]] && CURRENT_SONG=$(cat "$VIBE_DIR/now_playing") || CURRENT_SONG="None"
  [[ -f "$VIBE_DIR/state" ]] && STATE=$(cat "$VIBE_DIR/state") || STATE="stopped"
  [[ -f "$VIBE_DIR/vol" ]] && VOL=$(cat "$VIBE_DIR/vol") || VOL=0

  # Read head and strip decimals
  [[ -f "$VIBE_DIR/head" ]] && HEAD_RAW=$(cat "$VIBE_DIR/head") || HEAD_RAW="0.00"
  ELAPSED=${HEAD_RAW%.*}
  [[ -z "$ELAPSED" ]] && ELAPSED=0

  # 3. ASYNC METADATA HOOK
  if [[ "$CURRENT_SONG" != "$LAST_SONG" && "$CURRENT_SONG" != "None" ]]; then
    TITLE="Fetching tags..."
    DURATION=0
    LAST_SONG="$CURRENT_SONG"
    (
      D=$(ffprobe -v error -show_entries format=duration -of default=noprint_wrappers=1:nokey=1 "$MUSIC_DIR/$CURRENT_SONG" | cut -d. -f1)
      T=$(ffprobe -v error -show_entries format_tags=title -of default=noprint_wrappers=1:nokey=1 "$MUSIC_DIR/$CURRENT_SONG")
      [[ -z "$T" ]] && T="$CURRENT_SONG"
      echo "$D|$T" >"$META_CACHE"
    ) &
    TITLE=""
  fi

  # Update metadata from RAM cache
  if [[ -f "$META_CACHE" ]]; then
    META_DATA=$(cat "$META_CACHE")
    DURATION=$(echo "$META_DATA" | cut -d'|' -f1)
    TITLE=$(echo "$META_DATA" | cut -d'|' -f2)
    rm "$META_CACHE"
  fi

  # 4. RENDER UI
  tput cup 0 0
  echo -e "\e[1;35m--- Vibe-Core Dashboard ---\e[0m"
  echo -e "Song:   \e[1;37m${TITLE:0:45}\e[0m"

  # Status and Volume Row
  echo -ne "Status: "
  [[ "$STATE" == "playing" ]] && echo -ne "\e[32m$STATE\e[0m" || echo -ne "\e[31m$STATE\e[0m"

  # Show volume with a small indicator
  if [[ "$VOL" -eq 0 ]]; then
    echo -e "  |  Vol: \e[31mMUTE\e[0m    "
  else
    echo -e "  |  Vol: \e[1;36m$VOL%\e[0m   "
  fi

  # 5. PROGRESS BAR
  if [[ $DURATION -gt 0 ]]; then
    PCT=$((ELAPSED * 100 / DURATION))
    [[ $PCT -gt 100 ]] && PCT=100
    FILL=$((PCT * 30 / 100))
    printf "[%02d:%02d / %02d:%02d] [" $((ELAPSED / 60)) $((ELAPSED % 60)) $((DURATION / 60)) $((DURATION % 60))
    for ((i = 0; i < 30; i++)); do
      [[ $i -lt $FILL ]] && printf "#" || printf "-"
    done
    printf "] %d%%   \n" "$PCT"
  else
    echo -e "Time: [--:-- / --:--] [------------------------------] --%  "
  fi

  echo "-------------------------------------------"
  echo -e "[n] Next | [p] Prev | [t] Toggle | [m] Mute"
  echo -e "[+] Vol Up | [-] Vol Down | [q] Quit"

  # 6. INPUT HANDLING
  read -rsn 1 -t 0.1 key
  case "$key" in
  "n") echo "next" >"$VIBE_DIR/ctl" ;;
  "p") echo "prev" >"$VIBE_DIR/ctl" ;;
  "t") [[ "$STATE" == "playing" ]] && echo "pause" >"$VIBE_DIR/ctl" || echo "resume" >"$VIBE_DIR/ctl" ;;
  "m")
    if [[ "$VOL" -gt 0 ]]; then
      echo "$VOL" >"$MUTE_FILE"
      echo "0" >"$VIBE_DIR/vol"
    else
      [[ -f "$MUTE_FILE" ]] && PREV_VOL=$(cat "$MUTE_FILE") || PREV_VOL=50
      echo "$PREV_VOL" >"$VIBE_DIR/vol"
    fi
    ;;
  "+")
    NEW_VOL=$((VOL + 5))
    ((NEW_VOL > 100)) && NEW_VOL=100
    echo "$NEW_VOL" >"$VIBE_DIR/vol"
    ;;
  "-")
    NEW_VOL=$((VOL - 5))
    ((NEW_VOL < 0)) && NEW_VOL=0
    echo "$NEW_VOL" >"$VIBE_DIR/vol"
    ;;
  "q") break ;;
  esac
done
