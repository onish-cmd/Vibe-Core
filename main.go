package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/gen2brain/malgo"
	"github.com/hajimehoshi/go-mp3"
)

var (
	currentState   = "playing"
	currentSong    = "none"
	currentElapsed float64
	sampleRate     float64 = 48000
	mu             sync.Mutex
	volume         float32 = 0.5
	device         *malgo.Device
	decoder        *mp3.Decoder
	playlist       []string
	index          = 0
	skip           = make(chan bool, 1)
	musicDir       = "music"
	audioBuffer    = make(chan []byte, 512)
	playNowActive  = false
	savedIndex     int
	savedMusicDir  string
	sampleSum      float64
	sampleCount    int
	muSum          sync.Mutex
	needsRefresh   = true
	specChan       = make(chan []float64, 5)
)

func main() {
	setupFiles()

	ctx, _ := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	defer ctx.Free()

	// Start all background workers
	go watchVolume()
	go watchCtl()
	go watchPlayNow()
	go watchSeek()
	go startTelemetry()
	go watchMusicDir()

	for {
		playNext(ctx)
	}
}

func setupFiles() {
	shmPath := "/dev/shm/vibe"
	os.MkdirAll(shmPath, 0o777)
	nodes := []string{"ctl", "vol", "state", "now_playing", "head", "play_now", "seek", "len", "db"}

	for _, node := range nodes {
		fullShmPath := filepath.Join(shmPath, node)
		if node == "ctl" || node == "play_now" || node == "seek" {
			os.Remove(fullShmPath)
			syscall.Mkfifo(fullShmPath, 0o666)
		} else if node == "vol" {
			if _, err := os.Stat(fullShmPath); os.IsNotExist(err) {
				os.WriteFile(fullShmPath, []byte("50"), 0o644)
			}
		} else {
			os.WriteFile(fullShmPath, []byte(""), 0o644)
		}
		os.Remove(node)
		os.Symlink(fullShmPath, node)
	}
}

func cleanup() {
	fmt.Println("\n[VIBE] Cleaning up UAPI...")
	nodes := []string{"ctl", "vol", "state", "now_playing", "head", "play_now", "seek", "len", "db", "spectrum"}
	for _, n := range nodes {
		os.Remove(n)
	}
	os.RemoveAll("/dev/shm/vibe")
}
