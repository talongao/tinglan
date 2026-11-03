//go:build !darwin

package audio

import (
	"context"
	"errors"

	"tinglan/native/internal/resource"
)

// RemoteProbe is the network half of OpenRemote. Non-darwin backends use the
// portable player, so the probe only validates the transport and container.
type RemoteProbe struct {
	url string
}

// ProbeRemote performs the HTTP probe and format sniff. It is safe to call from
// a worker goroutine.
func ProbeRemote(ctx context.Context, url string) (*RemoteProbe, error) {
	client := resource.New()
	probe, err := client.Range(ctx, url, 0, 512)
	if err != nil {
		return nil, err
	}
	if len(probe.Bytes) >= 4 && string(probe.Bytes[:4]) == "fLaC" {
		return &RemoteProbe{url: url}, nil
	}
	if len(probe.Bytes) >= 3 && string(probe.Bytes[:3]) == "ID3" {
		return &RemoteProbe{url: url}, nil
	}
	if len(probe.Bytes) >= 2 && probe.Bytes[0] == 0xff && probe.Bytes[1]&0xe0 == 0xe0 {
		return &RemoteProbe{url: url}, nil
	}
	return nil, errors.New("unsupported audio format")
}

// Open starts playback on the portable backend.
func (p *RemoteProbe) Open() (*Player, error) {
	return OpenRemote(context.Background(), p.url)
}
