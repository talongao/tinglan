//go:build (linux || windows || portableaudio) && cgo

package audio

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"os"
	"sync"
	"time"

	"github.com/gen2brain/malgo"
	"github.com/gopxl/beep"
	"github.com/gopxl/beep/flac"
	"github.com/gopxl/beep/mp3"
	"tinglan/native/internal/resource"
)

type State struct {
	Position, Duration            float64
	Ready, Failed, Ended, Playing bool
}
type seekRequest struct {
	seconds float64
}
type Player struct {
	mu          sync.Mutex
	closeOnce   sync.Once
	state       State
	volume      float64
	source      io.ReadSeekCloser
	decoder     beep.StreamSeekCloser
	rate        int
	context     *malgo.AllocatedContext
	device      *malgo.Device
	ring        [32768][2]float64
	head, count int
	decodedEnd  bool
	wantPlaying bool
	seeking     bool
	stop        chan struct{}
	done        chan struct{}
	seeks       chan seekRequest
}

func OpenRemote(ctx context.Context, raw string) (*Player, error) {
	r, err := resource.OpenReader(ctx, raw)
	if err != nil {
		return nil, err
	}
	stop := context.AfterFunc(ctx, func() { _ = r.Close() })
	defer stop()
	return openPortable(r)
}
func OpenLocalFixture(path string) (*Player, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New("audio fixture unavailable")
	}
	return openPortable(f)
}
func openPortable(source io.ReadSeekCloser) (*Player, error) {
	var magic [4]byte
	if _, err := io.ReadFull(source, magic[:]); err != nil {
		source.Close()
		return nil, err
	}
	_, _ = source.Seek(0, io.SeekStart)
	var decoder beep.StreamSeekCloser
	var format beep.Format
	var err error
	if string(magic[:]) == "fLaC" {
		decoder, format, err = flac.Decode(source)
	} else if string(magic[:3]) == "ID3" || (magic[0] == 0xff && magic[1]&0xe0 == 0xe0) {
		decoder, format, err = mp3.Decode(source)
	} else {
		err = errors.New("unsupported audio format")
	}
	if err != nil {
		source.Close()
		return nil, errors.New("audio decoder unavailable")
	}
	p := &Player{source: source, decoder: decoder, rate: int(format.SampleRate), stop: make(chan struct{}), done: make(chan struct{}), seeks: make(chan seekRequest, 1)}
	p.state = State{Ready: true, Duration: float64(decoder.Len()) / float64(p.rate)}
	p.context, err = malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		decoder.Close()
		return nil, errors.New("audio output unavailable")
	}
	cfg := malgo.DefaultDeviceConfig(malgo.Playback)
	cfg.Playback.Format = malgo.FormatF32
	cfg.Playback.Channels = 2
	cfg.SampleRate = uint32(p.rate)
	cfg.Alsa.NoMMap = 1
	p.device, err = malgo.InitDevice(p.context.Context, cfg, malgo.DeviceCallbacks{Data: p.output})
	if err != nil {
		p.context.Uninit()
		p.context.Free()
		decoder.Close()
		return nil, errors.New("audio output unavailable")
	}
	go p.decode()
	if err = p.device.Start(); err != nil {
		p.Close()
		return nil, errors.New("audio output failed")
	}
	return p, nil
}
func (p *Player) output(out, _ []byte, frames uint32) {
	clear(out)
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.state.Playing {
		return
	}
	n := min(int(frames), p.count, len(out)/8)
	for i := 0; i < n; i++ {
		sample := p.ring[(p.head+i)%len(p.ring)]
		for channel := 0; channel < 2; channel++ {
			binary.LittleEndian.PutUint32(out[i*8+channel*4:], math.Float32bits(float32(sample[channel]*p.volume)))
		}
	}
	p.head = (p.head + n) % len(p.ring)
	p.count -= n
	p.state.Position += float64(n) / float64(p.rate)
	if p.count == 0 && p.decodedEnd {
		p.state.Ended = true
		p.state.Playing = false
	}
}
func (p *Player) decode() {
	defer close(p.done)
	defer p.decoder.Close()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	samples := make([][2]float64, 2048)
	for {
		select {
		case <-p.stop:
			return
		case request := <-p.seeks:
			p.mu.Lock()
			p.seeking = true
			p.state.Playing = false
			p.mu.Unlock()
			err := p.decoder.Seek(int(request.seconds * float64(p.rate)))
			p.mu.Lock()
			if err == nil {
				p.head = 0
				p.count = 0
				p.decodedEnd = false
				p.state.Ended = false
				p.state.Position = request.seconds
			}
			p.seeking = false
			p.state.Playing = p.wantPlaying && p.state.Ready && !p.state.Failed
			p.mu.Unlock()
		case <-ticker.C:
			p.mu.Lock()
			room := len(p.ring) - p.count
			ended := p.decodedEnd
			p.mu.Unlock()
			if room < len(samples) || ended {
				continue
			}
			n, ok := p.decoder.Stream(samples)
			p.mu.Lock()
			for i := 0; i < n; i++ {
				p.ring[(p.head+p.count+i)%len(p.ring)] = samples[i]
			}
			p.count += n
			if !ok {
				p.decodedEnd = true
				if p.decoder.Err() != nil {
					p.state.Failed = true
					p.state.Playing = false
				}
			}
			p.mu.Unlock()
		}
	}
}
func (p *Player) Play() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state.Ready && !p.state.Failed {
		p.wantPlaying = true
		p.state.Playing = !p.seeking
	}
}
func (p *Player) Pause() {
	p.mu.Lock()
	p.wantPlaying = false
	p.state.Playing = false
	p.mu.Unlock()
}
func (p *Player) Seek(seconds float64) error {
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < 0 {
		return errors.New("invalid seek")
	}
	p.mu.Lock()
	duration := p.state.Duration
	p.mu.Unlock()
	if duration > 0 {
		seconds = min(seconds, duration)
	}
	request := seekRequest{seconds: seconds}
	select {
	case p.seeks <- request:
		return nil
	case <-p.stop:
		return errors.New("player closed")
	default:
		return errors.New("seek already pending")
	}
}
func (p *Player) SetVolume(volume float64) error {
	if math.IsNaN(volume) || math.IsInf(volume, 0) || volume < 0 || volume > 1 {
		return errors.New("invalid volume")
	}
	p.mu.Lock()
	p.volume = volume
	p.mu.Unlock()
	return nil
}
func (p *Player) Snapshot() State { p.mu.Lock(); defer p.mu.Unlock(); return p.state }
func (p *Player) Close() {
	p.closeOnce.Do(func() {
		close(p.stop)
		p.source.Close()
		p.device.Uninit()
		<-p.done
		p.context.Uninit()
		p.context.Free()
		p.mu.Lock()
		p.state = State{}
		p.mu.Unlock()
	})
}
