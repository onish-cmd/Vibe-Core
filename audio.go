package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/gen2brain/malgo"
	"github.com/hajimehoshi/go-mp3"
)

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
				muSum.Lock()
				sampleSum += float64(res) * float64(res)
				sampleCount++
				muSum.Unlock()
			}
			readTotal += copyLen
			mu.Lock()
			if currentState == "playing" {
				currentElapsed += float64(frameCount) / sampleRate
			}
			mu.Unlock()

			floatSamples := make([]float64, len(pOutput)/2)
			for i := 0; i < len(pOutput); i += 2 {
				s := int16(binary.LittleEndian.Uint16(pOutput[i : i+2]))
				floatSamples[i/2] = float64(s)
			}
			select {
			case specChan <- floatSamples:
			default:
			}
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

func playNext(ctx *malgo.AllocatedContext) {
	mu.Lock()
	if needsRefresh {
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
		if len(playlist) == 1 && savedMusicDir != "" {
			playNowActive = true
		}
		needsRefresh = false
	}

	// Safety check for empty playlists
	if len(playlist) == 0 {
		mu.Unlock()
		return
	}

	filePath := playlist[index]
	f, err := os.Open(filepath.Join(musicDir, filePath))
	if err != nil {
		index = (index + 1) % len(playlist)
		mu.Unlock()
		return
	}
	d, _ := mp3.NewDecoder(f)
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
	deviceConfig.SampleRate, deviceConfig.Alsa.NoMMap, deviceConfig.PeriodSizeInFrames = sRate, 1, 1024

	device, _ = malgo.InitDevice(ctx.Context, deviceConfig, malgo.DeviceCallbacks{Data: onSamples})
	device.Start()

	mu.Lock()
	currentState, currentSong = "playing", filePath
	mu.Unlock()

	for {
		select {
		case <-skip:
			goto stop
		default:
			if len(audioBuffer) == 0 {
				goto stop
			}
			syscall.Select(0, nil, nil, nil, &syscall.Timeval{Usec: 100000})
		}
	}

stop:
	device.Uninit()
	close(stopDecode)
	f.Close()

	mu.Lock()
	if playNowActive && musicDir != savedMusicDir {
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
