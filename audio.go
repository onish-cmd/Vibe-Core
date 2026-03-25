package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gen2brain/malgo"
	"github.com/hajimehoshi/go-mp3"
)

func renderDecay(buffer []byte, startSample int16) {
	samples := len(buffer) / 2
	if samples == 0 {
		return
	}

	for i := 0; i < samples; i++ {
		// linear ramp to zero
		decayed := int16(float32(startSample) * (1.0 - float32(i)/float32(samples)))
		binary.LittleEndian.PutUint16(buffer[i*2:], uint16(decayed))
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
		case chunk, ok := <-audioBuffer:
			if !ok {
				renderDecay(pOutput[readTotal:], lastSample)
				lastSample = 0
				return
			}
			copyLen := len(chunk)
			if readTotal+copyLen > outputLen {
				copyLen = outputLen - readTotal
			}

			var chunkSum float64 // Local accumulator
			var lastRes int32

			for j := 0; j < copyLen; j += 2 {
				sample := int16(binary.LittleEndian.Uint16(chunk[j : j+2]))
				res := (int32(sample) * vFixed) >> 8

				// Fast clipping
				if res > 32767 {
					res = 32767
				} else if res < -32768 {
					res = -32768
				}

				binary.LittleEndian.PutUint16(pOutput[readTotal+j:], uint16(int16(res)))
				chunkSum += float64(res) * float64(res)
				lastRes = res
			}

			muSum.Lock()
			sampleSum += chunkSum
			sampleCount += copyLen / 2
			muSum.Unlock()

			readTotal += copyLen

			mu.Lock()
			lastSample = int16(lastRes)
			if currentState == "playing" {
				currentElapsed += float64(copyLen) / (sampleRate * 4)
			}
			mu.Unlock()

		default:
			renderDecay(pOutput[readTotal:], lastSample)
			lastSample = 0
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
			buffFill := len(audioBuffer)

			if buffFill > 500 {
				time.Sleep(200 * time.Millisecond)
			} else if buffFill > 200 {
				time.Sleep(40 * time.Millisecond)
			} else if buffFill > 100 {
				time.Sleep(10 * time.Millisecond)
			} else {
			}
			for i := 0; i < 10; i++ {
				buf := make([]byte, 4096)
				mu.Lock()
				n, err := io.ReadFull(d, buf)
				mu.Unlock()

				if n > 0 {
					audioBuffer <- buf[:n]
				}
				if err != nil {
					return
				}
			}
		}
	}
}

func savePlaylist(playlist []string) error {
	buf := new(bytes.Buffer)
	for _, s := range playlist {
		buf.WriteString(s)
		buf.WriteString("\n")
	}
	return os.WriteFile("/dev/shm/vibe/playlist", buf.Bytes(), 0o644)
}

func playNext(ctx *malgo.AllocatedContext) {
	mu.Lock()
	if needsRefresh {
		if len(playlist) == 1 && savedMusicDir != "" {
			playNowActive = true
		}
		if !playNowActive {
			index = 0
		}
		playlist = nil
		files, _ := os.ReadDir(musicDir)
		for _, f := range files {
			if strings.ToLower(filepath.Ext(f.Name())) == ".mp3" {
				playlist = append(playlist, f.Name())
			}
		}
		if len(playlist) == 0 {
			mu.Unlock()
			return
		}
		masterPlaylist = make([]string, len(playlist))
		copy(masterPlaylist, playlist)
		savePlaylist(playlist)
		needsRefresh = false
	}

	// Safety check for empty playlists
	if len(playlist) == 0 {
		mu.Unlock()
		fmt.Println("No MP3 found, quit!")
		cleanup()
		os.Exit(127)
	}

	filePath := playlist[index]
	f, err := os.Open(filepath.Join(musicDir, filePath))
	if err != nil {
		index = (index + 1) % len(playlist)
		mu.Unlock()
		return
	}
	d, err := mp3.NewDecoder(f)
	if err != nil || d == nil {
		fmt.Printf("[VIBE] Error: %s is not a valid MP3\n", filePath)
		f.Close()
		playNowActive = false
		index = (index + 1) % len(playlist)
		mu.Unlock()
		return
	}
	decoder = d
	sRate := uint32(d.SampleRate())
	sampleRate = float64(sRate)
	currentElapsed = 0
	mu.Unlock()

	for len(audioBuffer) > 0 {
		<-audioBuffer
	}
	stopDecode := make(chan bool)
	go decodeLoop(d, stopDecode)

	deviceConfig := malgo.DefaultDeviceConfig(malgo.Playback)
	deviceConfig.Playback.Format, deviceConfig.Playback.Channels = malgo.FormatS16, 2
	deviceConfig.SampleRate, deviceConfig.Alsa.NoMMap, deviceConfig.PeriodSizeInFrames = sRate, 1, 16384

	device, _ = malgo.InitDevice(ctx.Context, deviceConfig, malgo.DeviceCallbacks{Data: onSamples})
	device.Start()

	mu.Lock()
	currentState, currentSong = "playing", filePath
	mu.Unlock()

	for {
		select {
		case <-skip:
			goto stop
		case <-time.After(500 * time.Millisecond):
			mu.Lock()
			pos, _ := decoder.Seek(0, 1)
			if len(audioBuffer) == 0 && pos >= decoder.Length() {
				mu.Unlock()
				goto stop
			}
			mu.Unlock()
			time.Sleep(100 * time.Millisecond)
		}
	}

stop:
	device.Uninit()
	close(stopDecode)
	f.Close()

	mu.Lock()
	if playNowActive {
		musicDir = savedMusicDir
		index = savedIndex
		playNowActive = false
		needsRefresh = true // Force reload the original musicDir playlist
		fmt.Println("[VIBE] Play Now finished. Returning to original playlist.")
	} else {
		// Normal progression
		index = (index + 1) % len(playlist)
	}
	mu.Unlock()
}
