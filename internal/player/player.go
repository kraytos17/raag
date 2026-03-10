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
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if p.streamer != nil {
		speaker.Clear()
		if p.file != nil {
			p.file.Close()
		}
	}

	f, err := os.Open(song.Path)
	if err != nil {
		return fmt.Errorf("error opening audio file: %w", err)
	}

	streamer, format, err := p.decodeAudio(f)
	if err != nil {
		f.Close()
		return fmt.Errorf("error decoding audio file: %w", err)
	}

	p.file = f
	p.streamer = streamer
	p.format = format
	p.Position = 0
	p.Duration = int(time.Duration(streamer.Len()).Seconds() * float64(format.SampleRate) / float64(time.Second))

	inLoop := beep.Loop(-1, streamer)
	p.VolumeCtrl = &effects.Volume{Streamer: inLoop, Base: 2, Volume: 0}
	p.ctrl = &beep.Ctrl{Streamer: p.VolumeCtrl}
	speaker.Play(p.ctrl)

	fmt.Printf("Now playing: %s - %s\n", song.Title, song.Artist)
	return nil
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
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if len(p.Queue) == 0 {
		return fmt.Errorf("queue is empty")
	}

	p.CurrentIndex = 0
	return p.playSongAtIndexLocked(p.CurrentIndex)
}

func (p *Player) playSongAtIndexLocked(index int) error {
	if index < 0 || index >= len(p.Queue) {
		return fmt.Errorf("invalid index")
	}
	song := p.Queue[index]
	return p.Play(song)
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

	if p.ctrl != nil {
		speaker.Clear()
		p.ctrl = nil
	}

	p.Queue = []metadata.Song{}
	p.CurrentIndex = -1
	p.Position = 0
	fmt.Println("Playback stopped and queue cleared")
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

func (p *Player) ClearQueue() {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	p.Queue = []metadata.Song{}
	p.CurrentIndex = -1
}

func (p *Player) Next() error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if len(p.Queue) == 0 {
		return fmt.Errorf("queue is empty")
	}
	if p.CurrentIndex >= len(p.Queue)-1 {
		return fmt.Errorf("end of queue")
	}

	p.CurrentIndex++
	return p.playSongAtIndexLocked(p.CurrentIndex)
}

func (p *Player) Previous() error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if len(p.Queue) == 0 {
		return fmt.Errorf("queue is empty")
	}
	if p.CurrentIndex <= 0 {
		return fmt.Errorf("beginning of queue")
	}

	p.CurrentIndex--
	return p.playSongAtIndexLocked(p.CurrentIndex)
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
	if err := p.streamer.Seek(pos); err != nil {
		return fmt.Errorf("seek error: %w", err)
	}

	p.Position = seconds
	fmt.Printf("Seeked to %d seconds\n", seconds)
	return nil
}

func (p *Player) GetPosition() int {
	p.mutex.RLock()
	defer p.mutex.RUnlock()
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
