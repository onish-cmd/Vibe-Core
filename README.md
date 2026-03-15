# Vibe-Core

An ultra-lightweight, sample-accurate audio engine written in Go. Vibe-Core follows the **Unix Philosophy**: everything is a file. It exposes a **UAPI** (User API) through memory-mapped nodes in `/dev/shm`, allowing any process to control playback with simple file I/O.

## Features
- **Zero-Bloat IPC:** Uses FIFOs and shared memory for near-zero latency control.
- **Resource Efficient:** Designed to run on low-end hardware (tested on AMD E-350 with 2% CPU).
- **Direct Injection:** Support for immediate file playback without playlist corruption.
- **Sample-Accurate Telemetry:** High-precision playback tracking via `/dev/shm/vibe/head`.
- **Digital Boost:** Software-level gain up to 200%.

## UAPI Reference

Vibe-Core exposes interaction nodes via memory-backed files in `/dev/shm/vibe/`.

| Node | Mode | Description |
| :--- | :--- | :--- |
| `ctl` | W/O | **Command Pipe:** Real-time instructions (`next`, `prev`, `pause`, `resume`). |
| `seek` | W/O | **Seek Control:** Atomic jump to a timestamp in seconds (e.g., `echo "30.5" > seek`). |
| `vol` | R/W | **Volume Node:** Gain control (0-100 unity, 101-200 digital boost). Supports reading. |
| `play_now`| W/O | **Priority Injection:** Immediate playback of a file path without altering playlist state. |
| `head` | R/O | **Telemetry:** High-precision float representing current playback position in seconds. |
| `state` | R/O | **Engine Status:** Returns current playback state (`playing` or `paused`). |
| `now_playing`| R/O | **Metadata:** Returns the filename of the currently active audio stream. |

## Build & Run
```bash
go build -o vibe-core
./vibe-core
