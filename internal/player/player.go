package player

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/faiface/beep"
	"github.com/faiface/beep/effects"
	"github.com/faiface/beep/flac"
	"github.com/faiface/beep/mp3"
	"github.com/faiface/beep/speaker"
	"github.com/faiface/beep/vorbis"
	"github.com/faiface/beep/wav"
	"github.com/p-society/raag/internal/config"
	"github.com/p-society/raag/internal/logger"
	"github.com/p-society/raag/internal/metadata"
)

type Player struct {
	ctrl         *beep.Ctrl
	format       beep.Format
	streamer     beep.StreamSeeker
	streamCloser beep.StreamSeekCloser
	file         *os.File
	queue        []metadata.Song
	currentIndex int
	volume       float64
	position     int
	duration     int
	volumeCtrl   *effects.Volume
	mutex        sync.RWMutex
	playerNext   func() error
	trackEnded   chan struct{}
}

func NewPlayer() (*Player, error) {
	err := speaker.Init(44100, 44100/10)
	if err != nil {
		return nil, fmt.Errorf("error initializing speaker: %w", err)
	}
	return &Player{
		volume:       0.5,
		currentIndex: -1,
		queue:        []metadata.Song{},
	}, nil
}

func (p *Player) Play(song metadata.Song) error {
	cleanPath := filepath.Clean(song.Path)
	musicDir, err := config.MusicDir()
	if err != nil {
		return fmt.Errorf("failed to get music dir: %w", err)
	}

	root, err := os.OpenRoot(musicDir)
	if err != nil {
		return fmt.Errorf("error opening music root: %w", err)
	}

	relPath, err := filepath.Rel(musicDir, cleanPath)
	if err != nil {
		return fmt.Errorf("failed to get relative path: %w", err)
	}

	f, err := root.Open(relPath)
	if err != nil {
		return fmt.Errorf("error opening audio file: %w", err)
	}

	streamer, format, err := p.decodeAudio(f)
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("error decoding audio file: %w", err)
	}

	volume := p.currentVolume()
	duration := 0
	if streamer.Len() > 0 {
		duration = streamer.Len() / int(format.SampleRate)
	}

	p.mutex.Lock()
	p.stopPlaybackLocked() // Clean up old state first

	p.trackEnded = make(chan struct{}, 1)
	trackEnded := p.trackEnded

	p.file = f
	p.streamer = streamer
	p.streamCloser = streamer
	p.format = format
	p.position = 0
	p.duration = duration

	seq := beep.Seq(streamer, beep.Callback(func() {
		select {
		case trackEnded <- struct{}{}:
		default:
		}
	}))

	p.ctrl = &beep.Ctrl{Streamer: seq}
	p.volumeCtrl = &effects.Volume{Streamer: p.ctrl, Base: 2, Volume: volume}
	p.mutex.Unlock()

	speaker.Play(p.volumeCtrl)
	currentTrackEnded := trackEnded
	go func() {
		_, ok := <-currentTrackEnded
		if !ok {
			return
		}
		if p.playerNext != nil {
			if err := p.playerNext(); err != nil {
				logger.Debugf("Queue finished or error playing next: %v", err)
			}
		}
	}()

	fmt.Printf("Now playing: %s - %s\n", song.Title, song.Artist)
	return nil
}

func (p *Player) currentVolume() float64 {
	p.mutex.RLock()
	defer p.mutex.RUnlock()
	return p.volume
}

func (p *Player) decodeAudio(f *os.File) (beep.StreamSeekCloser, beep.Format, error) {
	return p.decodeAudioFromReader(f, filepath.Ext(f.Name()))
}

func (p *Player) PlayQueue() error {
	song, err := p.selectSongAtIndex(0)
	if err != nil {
		return err
	}
	return p.Play(song)
}

func (p *Player) selectSongAtIndex(index int) (metadata.Song, error) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if len(p.queue) == 0 {
		return metadata.Song{}, fmt.Errorf("queue is empty")
	}
	if index < 0 || index >= len(p.queue) {
		return metadata.Song{}, fmt.Errorf("invalid index")
	}

	p.currentIndex = index
	return p.queue[index], nil
}

func (p *Player) Pause() {
	p.mutex.RLock()
	ctrl := p.ctrl
	p.mutex.RUnlock()

	if ctrl != nil {
		speaker.Lock()
		ctrl.Paused = true
		speaker.Unlock()
		logger.Infof("Playback paused")
	}
}

func (p *Player) Resume() {
	p.mutex.RLock()
	ctrl := p.ctrl
	p.mutex.RUnlock()

	if ctrl != nil {
		speaker.Lock()
		ctrl.Paused = false
		speaker.Unlock()
		logger.Infof("Playback resumed")
	}
}

func (p *Player) Stop() {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	p.stopPlaybackLocked()
	p.queue = []metadata.Song{}
	p.currentIndex = -1
	p.position = 0
	p.duration = 0
	fmt.Println("Playback stopped and queue cleared")
}

func (p *Player) stopPlaybackLocked() {
	if p.ctrl != nil || p.streamer != nil {
		speaker.Clear()
	}
	if p.trackEnded != nil {
		close(p.trackEnded)
		p.trackEnded = nil
	}

	p.ctrl = nil
	p.streamer = nil
	p.volumeCtrl = nil
	p.format = beep.Format{}
	if p.streamCloser != nil {
		_ = p.streamCloser.Close()
		p.streamCloser = nil
	}
	if p.file != nil {
		_ = p.file.Close()
		p.file = nil
	}
}

func (p *Player) AddToQueue(song metadata.Song) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.queue = append(p.queue, song)
}

func (p *Player) GetQueue() []metadata.Song {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	result := make([]metadata.Song, len(p.queue))
	copy(result, p.queue)
	return result
}

func (p *Player) Next() error {
	p.mutex.Lock()
	if len(p.queue) == 0 {
		p.mutex.Unlock()
		return fmt.Errorf("queue is empty")
	}
	if p.currentIndex >= len(p.queue)-1 {
		p.mutex.Unlock()
		return fmt.Errorf("end of queue")
	}

	p.currentIndex++
	song := p.queue[p.currentIndex]
	p.mutex.Unlock()

	return p.Play(song)
}

func (p *Player) Previous() error {
	p.mutex.Lock()
	if len(p.queue) == 0 {
		p.mutex.Unlock()
		return fmt.Errorf("queue is empty")
	}
	if p.currentIndex <= 0 {
		p.mutex.Unlock()
		return fmt.Errorf("beginning of queue")
	}

	p.currentIndex--
	song := p.queue[p.currentIndex]
	p.mutex.Unlock()

	return p.Play(song)
}

func (p *Player) GetCurrentSong() *metadata.Song {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	if p.currentIndex < 0 || p.currentIndex >= len(p.queue) {
		return nil
	}

	song := p.queue[p.currentIndex]
	return &song
}

func (p *Player) SetVolume(level float64) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if level < 0 || level > 100 {
		return fmt.Errorf("volume must be between 0 and 100")
	}

	p.volume = (level/100 - 1) * 10
	if p.volumeCtrl != nil {
		speaker.Lock()
		p.volumeCtrl.Volume = p.volume
		speaker.Unlock()
	}
	fmt.Printf("Volume set to %.0f%%\n", level)
	return nil
}

func (p *Player) GetVolume() float64 {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	return (p.volume/10 + 1) * 100
}

func (p *Player) adjustVolume(delta float64) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	newVol := p.volume + (delta / 10)
	if newVol > 10 {
		newVol = 10
	} else if newVol < -10 {
		newVol = -10
	}

	p.volume = newVol
	if p.volumeCtrl != nil {
		speaker.Lock()
		p.volumeCtrl.Volume = p.volume
		speaker.Unlock()
	}
	fmt.Printf("Volume: %.0f%%\n", (newVol/10+1)*100)
}

func (p *Player) VolumeUp(amount float64) {
	p.adjustVolume(amount)
}

func (p *Player) VolumeDown(amount float64) {
	p.adjustVolume(-amount)
}

func (p *Player) Seek(seconds int) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if p.streamer == nil {
		return fmt.Errorf("no song playing")
	}

	pos := p.format.SampleRate.N(time.Duration(seconds) * time.Second)
	if p.streamer.Len() > 0 && pos > p.streamer.Len() {
		pos = p.streamer.Len()
	}

	speaker.Lock()
	err := p.streamer.Seek(pos)
	speaker.Unlock()

	if err != nil {
		return fmt.Errorf("seek error: %w", err)
	}

	p.position = pos / int(p.format.SampleRate)
	fmt.Printf("Seeked to %d seconds\n", seconds)
	return nil
}

func (p *Player) GetPosition() int {
	p.mutex.RLock()
	defer p.mutex.RUnlock()
	if p.streamer != nil && p.format.SampleRate > 0 {
		speaker.Lock()
		pos := p.streamer.Position()
		speaker.Unlock()
		return pos / int(p.format.SampleRate)
	}
	return p.position
}

func (p *Player) GetDuration() int {
	p.mutex.RLock()
	defer p.mutex.RUnlock()
	return p.duration
}

func (p *Player) IsPlaying() bool {
	p.mutex.RLock()
	defer p.mutex.RUnlock()
	return p.ctrl != nil && !p.ctrl.Paused
}

func (p *Player) IsPaused() bool {
	p.mutex.RLock()
	defer p.mutex.RUnlock()
	return p.ctrl != nil && p.ctrl.Paused
}

func (p *Player) SetNextCallback(fn func() error) {
	p.playerNext = fn
}

// decodeAudioFromReader decodes an audio stream from an io.ReadSeeker.
// The ext parameter (e.g. ".mp3", ".flac") determines the decoder.
func (p *Player) decodeAudioFromReader(r io.ReadSeeker, ext string) (beep.StreamSeekCloser, beep.Format, error) {
	// mp3 and vorbis decoders require io.ReadCloser; wrap with a no-op closer.
	rc := io.NopCloser(r).(interface {
		io.ReadCloser
		io.Seeker
	})
	_ = rc // keep compiler happy — we use rc only for mp3/vorbis below

	switch strings.ToLower(ext) {
	case ".mp3":
		return mp3.Decode(struct {
			io.ReadCloser
			io.Seeker
		}{io.NopCloser(r), r})
	case ".flac":
		return flac.Decode(r)
	case ".wav":
		return wav.Decode(r)
	case ".ogg", ".ogv":
		return vorbis.Decode(struct {
			io.ReadCloser
			io.Seeker
		}{io.NopCloser(r), r})
	default:
		return nil, beep.Format{}, fmt.Errorf("unsupported audio format: %s", ext)
	}
}

// PlayFromReader plays a song whose audio bytes are provided by an io.ReadSeeker
// song.Path is used only to determine the audio format via its extension.
func (p *Player) PlayFromReader(r io.ReadSeeker, song metadata.Song) error {
	ext := strings.ToLower(filepath.Ext(song.Path))
	if ext == "" {
		return fmt.Errorf("cannot determine audio format: no extension in path %s", song.Path)
	}

	streamer, format, err := p.decodeAudioFromReader(r, ext)
	if err != nil {
		return fmt.Errorf("error decoding audio stream: %w", err)
	}

	volume := p.currentVolume()
	duration := 0
	if streamer.Len() > 0 {
		duration = streamer.Len() / int(format.SampleRate)
	}

	p.mutex.Lock()
	p.stopPlaybackLocked()

	p.trackEnded = make(chan struct{}, 1)
	trackEnded := p.trackEnded

	// No backing *os.File for a reader-based stream
	p.file = nil
	p.streamer = streamer
	p.streamCloser = streamer
	p.format = format
	p.position = 0
	p.duration = duration

	seq := beep.Seq(streamer, beep.Callback(func() {
		select {
		case trackEnded <- struct{}{}:
		default:
		}
	}))

	p.ctrl = &beep.Ctrl{Streamer: seq}
	p.volumeCtrl = &effects.Volume{Streamer: p.ctrl, Base: 2, Volume: volume}
	p.mutex.Unlock()

	speaker.Play(p.volumeCtrl)
	currentTrackEnded := trackEnded
	go func() {
		_, ok := <-currentTrackEnded
		if !ok {
			return
		}
		if p.playerNext != nil {
			if err := p.playerNext(); err != nil {
				logger.Debugf("Queue finished or error playing next: %v", err)
			}
		}
	}()

	fmt.Printf("Now playing (stream): %s - %s\n", song.Title, song.Artist)
	return nil
}
