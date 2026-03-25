# Vibe-Core <img src="https://img.shields.io/badge/Version-1.5.0-7aa2f7?style=flat-square" alt="v1.5.0">

**Vibe-Core** is a headless audio engine written in Go, designed specifically for legacy hardware. It follows the **Unix Philosophy** which is: *Everything is a file,* and *Do one thing and do it well.*

By exposing a **UAPI** (User API) through memory-mapped nodes in `/dev/shm`, Vibe-Core allows any process—from a heavy GUI to a simple shell script—to control playback with zero-copy overhead.

## Why Vibe-Core?

Unlike standard players (MPD, cmus) that rely on hundreds of shared libraries, Vibe-Core is built for **survival**:
- **Minimal Dependencies:** Linked only against `libc` and `libm`. It runs even when your `/usr/lib` is corrupted.
- **Legacy Optimized:** Tuned for legacy hardware runs on almost anything from a **AMD E-350** to a **Ryzen 7 9800X3D**.
- **Resilient Memory:** Uses a 512-chunk buffer (~10s safety net) to prevent stutters during heavy system load.
- **Zero-Network IPC:** No TCP sockets. All control happens via Shared Memory and FIFOs in `/dev/shm/vibe/`.

## Performance (v1.5.0)

| Metric | Value | Note |
| :--- | :--- | :--- |
| **Binary Size** | ~4.8 MB | Static-linked components for portability |
| **Avg CPU (E-350)** | 15% - 18% | Stabilized via Buffer-Aware Throttling |
| **Idle CPU** | 13% | Optimized context switching |
| **Telemetry Sync** | 500ms | Balanced for performance vs. visibility |

## UAPI Quick Start

You can control the engine using standard shell commands, for instance:

```bash
# Playback Control
echo "pause" > /dev/shm/vibe/ctl
echo "resume" > /dev/shm/vibe/ctl

# Volume (Fixed-Point Scaling)
echo "75" > /dev/shm/vibe/vol

# Telemetry Peek
cat /dev/shm/vibe/now_playing
cat /dev/shm/vibe/head # Current position in seconds
```

## Documentation
Documentation can be found [here](https://onish-cmd.github.io/Vibe-Core/)
