package main

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
	skip           = make(chan bool)
	musicDir       = "music"
	audioBuffer    = make(chan []byte, 512)
	playNowActive  = false
	savedIndex     int
	savedMusicDir  string
)

func updateUAPI() {
	os.WriteFile("/dev/shm/vibe/state", []byte(currentState), 0o644)
	os.WriteFile("/dev/shm/vibe/now_playing", []byte(currentSong), 0o644)
}

func watchPlayNow() {
	for {
		f, _ := os.OpenFile("play_now", os.O_RDONLY, 0)
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			path := strings.TrimSpace(scanner.Text())
			if _, err := os.Stat(path); err == nil {
				mu.Lock()
				if !playNowActive {
					savedIndex = index
					savedMusicDir = musicDir
				}
				playlist = []string{filepath.Base(path)}
				musicDir = filepath.Dir(path)
				index = 0
				playNowActive = true
				mu.Unlock()

				skip <- true // Force engine to reload with this new path
			}
		}
		f.Close()
	}
}

func watchSeek() {
    for {
        // Blocks here until someone echoes to the pipe
        f, err := os.OpenFile("seek", os.O_RDONLY, 0)
        if err != nil {
            continue 
        }
        
        scanner := bufio.NewScanner(f)
        for scanner.Scan() {
            input := strings.TrimSpace(scanner.Text())
            if sec, err := strconv.ParseFloat(input, 64); err == nil {
                bytePos := int64(sec * sampleRate * 4)

                mu.Lock()
                // Seek the underlying PCM stream
                _, err := decoder.Seek(bytePos, io.SeekStart)
                if err == nil {
                    // Flush the buffer
                    for len(audioBuffer) > 0 {
                        <-audioBuffer
                    }
                    currentElapsed = sec
                }
                mu.Unlock()
            }
        }
        f.Close()
    }
}

func onSamples(pOutput, pInput []byte, frameCount uint32) {
	outputLen := len(pOutput)
	readTotal := 0

	mu.Lock()
	vFixed := int32(volume * 256)
	mu.Unlock()

	for readTotal < outputLen {
		select {
		case chunk := <-audioBuffer:
			copyLen := len(chunk)
			if readTotal+copyLen > outputLen {
				copyLen = outputLen - readTotal
			}

			for j := 0; j < copyLen; j += 2 {
				sample := int16(binary.LittleEndian.Uint16(chunk[j : j+2]))
				res := (int32(sample) * vFixed) >> 8

				if res > 32767 {
					res = 32767
				} else if res < -32768 {
					res = -32768
				}
				binary.LittleEndian.PutUint16(pOutput[readTotal+j:], uint16(int16(res)))
			}
			readTotal += copyLen
			mu.Lock()
			if currentState == "playing" {
				currentElapsed += float64(frameCount) / sampleRate
			}
			os.WriteFile("/dev/shm/vibe/head", []byte(fmt.Sprintf("%.2f", currentElapsed)), 0o644)
			mu.Unlock()
		default:
			for i := readTotal; i < outputLen; i++ {
				pOutput[i] = 0
			}
			return
		}
	}
}

func decodeLoop(d *mp3.Decoder, stop chan bool) {
	for {
		select {
		case <-stop:
			return
		default:
			buf := make([]byte, 4096)
			n, err := io.ReadFull(d, buf)
			if n > 0 {
				audioBuffer <- buf[:n]
			}
			if err != nil {
				return
			}
		}
	}
}

func playNext(ctx *malgo.AllocatedContext) {
	mu.Lock()
	if playNowActive {
		playNowActive = false
	} else {
		if savedMusicDir != "" {
			musicDir = savedMusicDir
			index = savedIndex
			savedMusicDir = ""
		}
		playlist = nil
		files, _ := os.ReadDir("music")
		for _, f := range files {
			if strings.ToLower(filepath.Ext(f.Name())) == ".mp3" {
				playlist = append(playlist, f.Name())
			}
		}

		if len(playlist) == 0 {
			fmt.Println("No MP3s found.")
			return
		}
	}
	filePath := playlist[index]
	f, err := os.Open(filepath.Join(musicDir, filePath))
	if err != nil {
		index = (index + 1) % len(playlist)
		return
	}
	defer f.Close()

	d, err := mp3.NewDecoder(f)
	if err != nil {
		index = (index + 1) % len(playlist)
		return
	}

	sRate := uint32(d.SampleRate())
	sampleRate = float64(sRate)
	currentElapsed = 0
	mu.Unlock()

	// Drain old buffer data
	for len(audioBuffer) > 0 {
		<-audioBuffer
	}

	stopDecode := make(chan bool)
	go decodeLoop(d, stopDecode)

	for len(audioBuffer) < 1 {
		syscall.Select(0, nil, nil, nil, &syscall.Timeval{Usec: 10000})
	}

	deviceConfig := malgo.DefaultDeviceConfig(malgo.Playback)
	deviceConfig.Playback.Format = malgo.FormatS16
	deviceConfig.Playback.Channels = 2
	deviceConfig.SampleRate = sRate
	deviceConfig.Alsa.NoMMap = 1
	deviceConfig.PeriodSizeInFrames = 1024

	device, err = malgo.InitDevice(ctx.Context, deviceConfig, malgo.DeviceCallbacks{Data: onSamples})
	if err != nil {
		close(stopDecode)
		return
	}

	fmt.Printf("\n[VIBE] %s (%dHz)\n", filePath, sRate)
	device.Start()
	mu.Lock()
	currentState = "playing"
	currentSong = filePath
	mu.Unlock()
	updateUAPI()

	for {
		select {
		case <-skip:
			goto stop
		default:
			if len(audioBuffer) == 0 {
				fmt.Println("Song end")
				goto stop
			}
			syscall.Select(0, nil, nil, nil, &syscall.Timeval{Usec: 100000})
		}
	}

stop:
	device.Uninit()
	close(stopDecode)
	mu.Lock()
	index = (index + 1) % len(playlist)
	mu.Unlock()
}

func main() {
	setupFiles()

	ctx, _ := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	defer ctx.Free()

	go watchVolume()
	go watchCtl()
	go watchPlayNow()
	go watchSeek()

	for {
		playNext(ctx)
	}
}

func setupFiles() {
	shmPath := "/dev/shm/vibe"
	os.MkdirAll(shmPath, 0o777)

	nodes := []string{"ctl", "vol", "state", "now_playing", "head", "play_now", "seek"}

	for _, node := range nodes {
		fullShmPath := filepath.Join(shmPath, node)

		// Setup the actual RAM-backed nodes
		if node == "ctl" || node == "play_now" || node == "seek" {
			os.Remove(fullShmPath)
			syscall.Mkfifo(fullShmPath, 0o666)
		} else if node == "vol" {
			if _, err := os.Stat(fullShmPath); os.IsNotExist(err) {
				os.WriteFile(fullShmPath, []byte("50"), 0o644)
			}
		} else if node == "head" {
			// Initialize head at 0 so hooks don't read an empty file
			os.WriteFile(fullShmPath, []byte("0.00"), 0o644)
		} else {
			os.WriteFile(fullShmPath, []byte(""), 0o644)
		}

		os.Remove(node) // Clear any old local files/links
		err := os.Symlink(fullShmPath, node)
		if err != nil {
			fmt.Printf("[!] Failed to symlink %s: %v\n", node, err)
		}
	}
}

func cleanup() {
	fmt.Println("\n[VIBE] Cleaning up UAPI...")
	os.Remove("ctl")
	os.Remove("vol")
	os.Remove("state")
	os.Remove("now_playing")
	os.Remove("head")
	os.RemoveAll("/dev/shm/vibe")
}

func watchVolume() {
	for {
		data, _ := os.ReadFile("vol")
		if v, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
			mu.Lock()
			volume = float32(v) / 100.0
			mu.Unlock()
		}
		syscall.Select(0, nil, nil, nil, &syscall.Timeval{Sec: 0, Usec: 500000})
	}
}

func watchCtl() {
	for {
		f, _ := os.OpenFile("ctl", os.O_RDONLY, 0)
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			switch strings.TrimSpace(scanner.Text()) {
			case "pause":
				if device != nil {
					device.Stop()
					mu.Lock()
					currentState = "paused"
					mu.Unlock()
					updateUAPI()
				}
			case "resume":
				if device != nil {
					device.Start()
					mu.Lock()
					currentState = "playing"
					mu.Unlock()
					updateUAPI()
				}
			case "next":
				skip <- true
			case "prev":
				mu.Lock()
				index = (index - 2 + len(playlist)) % len(playlist)
				mu.Unlock()
				skip <- true
			case "exit":
				cleanup()
				fmt.Println("Finished.")
				os.Exit(0)
			}
			f.Close()
		}
	}
}
