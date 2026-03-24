package main

import (
	"bufio"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

func watchPlayNow() {
	for {
		f, err := os.OpenFile("play_now", os.O_RDONLY, 0)
		if err != nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			path := strings.TrimSpace(scanner.Text())
			if _, err := os.Stat(path); err == nil {
				mu.Lock()
				if !playNowActive {
					savedIndex, savedMusicDir = index, musicDir
				}
				playlist = []string{filepath.Base(path)}
				musicDir, index, needsRefresh = filepath.Dir(path), 0, true
				mu.Unlock()
				select {
				case skip <- true:
				default:
				}
			}
		}
		f.Close()
	}
}

func startTelemetry() {
	ticker := time.NewTicker(50 * time.Millisecond)
	for range ticker.C {
		mu.Lock()
		elapsed, status, song := currentElapsed, currentState, currentSong
		var total float64
		if decoder != nil {
			total = float64(decoder.Length()) / (sampleRate * 4)
		}
		mu.Unlock()

		muSum.Lock()
		var db float64 = -100.0
		if sampleCount > 0 {
			rms := math.Sqrt(sampleSum / float64(sampleCount))
			if rms > 0 {
				db = 20 * math.Log10(rms/32768.0)
			}
			sampleSum, sampleCount = 0, 0
			os.WriteFile("/dev/shm/vibe/db", []byte(fmt.Sprintf("%.1f", db)), 0o644)
		}
		muSum.Unlock()

		os.WriteFile("/dev/shm/vibe/head", []byte(fmt.Sprintf("%.2f", elapsed)), 0o644)
		os.WriteFile("/dev/shm/vibe/len", []byte(fmt.Sprintf("%.2f", total)), 0o644)
		os.WriteFile("/dev/shm/vibe/state", []byte(status), 0o644)
		os.WriteFile("/dev/shm/vibe/now_playing", []byte(song), 0o644)
	}
}

func watchVolume() {
	for {
		data, _ := os.ReadFile("vol")
		mu.Lock()
		if v, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
			volume = float32(v) / 100.0
		}
		mu.Unlock()
		time.Sleep(500 * time.Millisecond)
	}
}

func watchCtl() {
	for {
		f, err := os.OpenFile("ctl", os.O_RDONLY, 0)
		if err != nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			cmd := strings.TrimSpace(scanner.Text())
			switch cmd {
			case "pause":
				if device != nil {
					device.Stop()
					mu.Lock()
					currentState = "paused"
					mu.Unlock()
				}
			case "resume":
				if device != nil {
					device.Start()
					mu.Lock()
					currentState = "playing"
					mu.Unlock()
				}
			case "next":
				skip <- true
			case "shuffle":
				shuffleMode = !shuffleMode
				if shuffleMode {
					mu.Lock()
					r := rand.New(rand.NewSource(time.Now().UnixNano()))
					r.Shuffle(len(playlist), func(i, j int) {
						playlist[i], playlist[j] = playlist[j], playlist[i]
					})
					savePlaylist(playlist)
					mu.Unlock()
				} else {
					playlist = make([]string, len(masterPlaylist))
					copy(playlist, masterPlaylist)

					for i, name := range playlist {
						if name == currentSong {
							index = i
							break
						}
					}
					savePlaylist(playlist)
				}
				status := "off"
				if shuffleMode {
					status = "on"
				}
				os.WriteFile("/dev/shm/vibe/shuffle_state", []byte(status), 0o644)
			case "prev":
				mu.Lock()
				index = (index - 2 + len(playlist)) % len(playlist)
				mu.Unlock()
				select {
				case skip <- true:
				default:
				}
			case "exit":
				cleanup()
				os.Exit(0)
			}
		}
		f.Close()
	}
}

func watchSeek() {
	for {
		// This blocks until someone writes to the 'seek' pipe
		f, err := os.OpenFile("seek", os.O_RDONLY, 0)
		if err != nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			input := strings.TrimSpace(scanner.Text())
			sec, err := strconv.ParseFloat(input, 64)
			if err != nil {
				continue
			}

			mu.Lock()
			if decoder == nil {
				mu.Unlock()
				continue
			}

			// Convert seconds to byte position (SampleRate * 4 bytes per frame)
			bytePos := int64(sec * sampleRate * 4)
			if bytePos > +decoder.Length() {
				fmt.Println("Seek out of bounds.")
				mu.Unlock()
				return
			}
			if bytePos >= 0 && bytePos <= decoder.Length() {
				_, err := decoder.Seek(bytePos, 0) // 0 = io.SeekStart
				if err == nil {
					// DRAIN the old audio buffer so the seek is instant
				drain:
					for {
						select {
						case <-audioBuffer:
						default:
							break drain
						}
					}
					currentElapsed = sec
				}
			}
			mu.Unlock()
		}
		f.Close()
	}
}

func watchMusicDir() {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		fmt.Println("[VIBE] inotify init failed:", err)
		return
	}
	defer watcher.Close()

	// Initial watch on the starting music directory
	err = watcher.Add(musicDir)
	if err != nil {
		fmt.Println("[VIBE] Failed to watch dir:", err)
	}

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			// If files are created, removed, or renamed, signal a refresh
			if event.Has(fsnotify.Create) || event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
				mu.Lock()
				needsRefresh = true
				mu.Unlock()
				fmt.Println("[VIBE] Music directory changed, refresh queued.")
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			fmt.Println("[VIBE] inotify error:", err)
		}
	}
}
