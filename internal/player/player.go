package player

import (
	"fmt"
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
	"github.com/p-society/raag/internal/metadata"
)

type Player struct {
	ctrl         *beep.Ctrl
	format       beep.Format
	streamer     beep.StreamSeeker
	streamCloser beep.StreamSeekCloser
	file         *os.File
	Queue        []metadata.Song
	CurrentIndex int
	Volume       float64
	Position     int
	Duration     int
	VolumeCtrl   *effects.Volume
	mutex        sync.RWMutex
}

func NewPlayer() (*Player, error) {
	err := speaker.Init(44100, 44100/10)
	if err != nil {
		return nil, fmt.Errorf("error initializing speaker: %w", err)
	}
	return &Player{
		Volume:       0.5,
		CurrentIndex: -1,
		Queue:        []metadata.Song{},
	}, nil
}

func (p *Player) Play(song metadata.Song) error {
	f, err := os.Open(song.Path)
	if err != nil {
		return fmt.Errorf("error opening audio file: %w", err)
	}

	streamer, format, err := p.decodeAudio(f)
	if err != nil {
		f.Close()
		return fmt.Errorf("error decoding audio file: %w", err)
	}

	volume := p.currentVolume()
	duration := 0
	if streamer.Len() > 0 {
		duration = streamer.Len() / int(format.SampleRate)
	}

	p.mutex.Lock()
	p.stopPlaybackLocked()

	p.file = f
	p.streamer = streamer
	p.streamCloser = streamer
	p.format = format
	p.Position = 0
	p.Duration = duration

	inLoop := beep.Loop(-1, streamer)
	p.VolumeCtrl = &effects.Volume{Streamer: inLoop, Base: 2, Volume: volume}
	p.ctrl = &beep.Ctrl{Streamer: p.VolumeCtrl}
	p.mutex.Unlock()

	speaker.Play(p.ctrl)
	fmt.Printf("Now playing: %s - %s\n", song.Title, song.Artist)
	return nil
}

func (p *Player) currentVolume() float64 {
	p.mutex.RLock()
	defer p.mutex.RUnlock()
	return p.Volume
}

func (p *Player) decodeAudio(f *os.File) (beep.StreamSeekCloser, beep.Format, error) {
	ext := strings.ToLower(filepath.Ext(f.Name()))
	switch ext {
	case ".mp3":
		return mp3.Decode(f)
	case ".flac":
		return flac.Decode(f)
	case ".wav":
		return wav.Decode(f)
	case ".ogg", ".ogv":
		return vorbis.Decode(f)
	default:
		return nil, beep.Format{}, fmt.Errorf("unsupported audio format: %s", ext)
	}
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

	if len(p.Queue) == 0 {
		return metadata.Song{}, fmt.Errorf("queue is empty")
	}
	if index < 0 || index >= len(p.Queue) {
		return metadata.Song{}, fmt.Errorf("invalid index")
	}

	p.CurrentIndex = index
	return p.Queue[index], nil
}

func (p *Player) Pause() {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if p.ctrl != nil {
		speaker.Lock()
		p.ctrl.Paused = true
		speaker.Unlock()
		fmt.Println("Playback paused")
	}
}

func (p *Player) Resume() {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if p.ctrl != nil {
		speaker.Lock()
		p.ctrl.Paused = false
		speaker.Unlock()
		fmt.Println("Playback resumed")
	}
}

func (p *Player) Stop() {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	p.stopPlaybackLocked()
	p.Queue = []metadata.Song{}
	p.CurrentIndex = -1
	p.Position = 0
	p.Duration = 0
	fmt.Println("Playback stopped and queue cleared")
}

func (p *Player) stopPlaybackLocked() {
	if p.ctrl != nil || p.streamer != nil {
		speaker.Clear()
	}

	p.ctrl = nil
	p.streamer = nil
	p.VolumeCtrl = nil
	p.format = beep.Format{}
	if p.streamCloser != nil {
		p.streamCloser.Close()
		p.streamCloser = nil
	}
	if p.file != nil {
		p.file.Close()
		p.file = nil
	}
}

func (p *Player) AddToQueue(song metadata.Song) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	p.Queue = append(p.Queue, song)
}

func (p *Player) GetQueue() []metadata.Song {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	result := make([]metadata.Song, len(p.Queue))
	copy(result, p.Queue)
	return result
}

func (p *Player) Next() error {
	p.mutex.RLock()
	currentIndex := p.CurrentIndex
	queueLen := len(p.Queue)
	p.mutex.RUnlock()

	if queueLen == 0 {
		return fmt.Errorf("queue is empty")
	}
	if currentIndex >= queueLen-1 {
		return fmt.Errorf("end of queue")
	}

	song, err := p.selectSongAtIndex(currentIndex + 1)
	if err != nil {
		return err
	}
	return p.Play(song)
}

func (p *Player) Previous() error {
	p.mutex.RLock()
	currentIndex := p.CurrentIndex
	queueLen := len(p.Queue)
	p.mutex.RUnlock()

	if queueLen == 0 {
		return fmt.Errorf("queue is empty")
	}
	if currentIndex <= 0 {
		return fmt.Errorf("beginning of queue")
	}

	song, err := p.selectSongAtIndex(currentIndex - 1)
	if err != nil {
		return err
	}
	return p.Play(song)
}

func (p *Player) GetCurrentSong() *metadata.Song {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	if p.CurrentIndex < 0 || p.CurrentIndex >= len(p.Queue) {
		return nil
	}

	song := p.Queue[p.CurrentIndex]
	return &song
}

func (p *Player) SetVolume(level float64) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if level < 0 || level > 100 {
		return fmt.Errorf("volume must be between 0 and 100")
	}

	p.Volume = (level/100 - 1) * 10
	if p.VolumeCtrl != nil {
		p.VolumeCtrl.Volume = p.Volume
	}
	fmt.Printf("Volume set to %.0f%%\n", level)
	return nil
}

func (p *Player) GetVolume() float64 {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	return (p.Volume + 1) * 100
}

func (p *Player) VolumeUp(amount float64) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	newVol := p.Volume + (amount / 10)
	if newVol > 10 {
		newVol = 10
	}

	p.Volume = newVol
	if p.VolumeCtrl != nil {
		p.VolumeCtrl.Volume = p.Volume
	}

	fmt.Printf("Volume: %.0f%%\n", (newVol/10+1)*100)
}

func (p *Player) VolumeDown(amount float64) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	newVol := p.Volume - (amount / 10)
	if newVol < -10 {
		newVol = -10
	}

	p.Volume = newVol
	if p.VolumeCtrl != nil {
		p.VolumeCtrl.Volume = p.Volume
	}

	fmt.Printf("Volume: %.0f%%\n", (newVol/10+1)*100)
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
	if err := p.streamer.Seek(pos); err != nil {
		return fmt.Errorf("seek error: %w", err)
	}

	p.Position = pos / int(p.format.SampleRate)
	fmt.Printf("Seeked to %d seconds\n", seconds)
	return nil
}

func (p *Player) GetPosition() int {
	p.mutex.RLock()
	defer p.mutex.RUnlock()
	if p.streamer != nil && p.format.SampleRate > 0 {
		return p.streamer.Position() / int(p.format.SampleRate)
	}
	return p.Position
}

func (p *Player) GetDuration() int {
	p.mutex.RLock()
	defer p.mutex.RUnlock()
	return p.Duration
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
