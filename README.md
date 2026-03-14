# Vibe-Core

An ultra-lightweight, sample-accurate audio engine written in Go. Vibe-Core follows the **Unix Philosophy**: everything is a file. It exposes a **UAPI** (User API) through memory-mapped nodes in `/dev/shm`, allowing any process to control playback with simple file I/O.

## Features
- **Zero-Bloat IPC:** Uses FIFOs and shared memory for near-zero latency control.
- **Resource Efficient:** Designed to run on low-end hardware (tested on AMD E-350 with 2% CPU).
- **Direct Injection:** Support for immediate file playback without playlist corruption.
- **Sample-Accurate Telemetry:** High-precision playback tracking via `/dev/shm/vibe/head`.
- **Digital Boost:** Software-level gain up to 200%.

## Architecture
Vibe-Core creates a virtual filesystem interface. 
- `ctl`: Control pipe (next, prev, pause, resume).
- `vol`: Gain control (0-200).
- `play_now`: Priority interrupt for specific file paths.
- `head`: Live playback timer.

## Build & Run
```bash
go build -o vibe-core
./vibe-core
