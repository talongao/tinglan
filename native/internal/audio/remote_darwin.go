//go:build darwin && cgo && !portableaudio

package audio

/*
#include <stdint.h>
#include <stdlib.h>
void *hm_audio_remote(uintptr_t handle, long long total, const char *type);
*/
import "C"

import (
	"context"
	"errors"
	"runtime/cgo"
	"sync"
	"unsafe"

	"tinglan/native/internal/resource"
)

type remoteSource struct {
	ctx      context.Context
	cancel   context.CancelFunc
	client   *resource.Client
	url      string
	mu       sync.Mutex
	requests map[uint64]context.CancelFunc
}

// RemoteProbe is the network half of OpenRemote: it checks the allowlisted
// transport and the container signature away from the main thread.
type RemoteProbe struct {
	client *resource.Client
	url    string
	total  int64
	kind   string
}

// ProbeRemote performs the HTTP probe and format sniff. It is safe to call from
// a worker goroutine and does not touch AVFoundation.
func ProbeRemote(ctx context.Context, url string) (*RemoteProbe, error) {
	client := resource.New()
	probe, err := client.Range(ctx, url, 0, 512)
	if err != nil {
		return nil, err
	}
	kind := "public.mp3"
	if len(probe.Bytes) >= 4 && string(probe.Bytes[:4]) == "fLaC" {
		kind = "org.xiph.flac"
	} else if !(len(probe.Bytes) >= 3 && string(probe.Bytes[:3]) == "ID3") && !(len(probe.Bytes) >= 2 && probe.Bytes[0] == 0xff && probe.Bytes[1]&0xe0 == 0xe0) {
		return nil, errors.New("unsupported audio format")
	}
	return &RemoteProbe{client: client, url: url, total: probe.Total, kind: kind}, nil
}

// Open builds the AVPlayer item. AVFoundation only reports progress when the
// item is created on a thread with a running run loop (the application main
// thread); creating it on a worker goroutine leaves the item stuck at
// status unknown with a zero duration.
func (r *RemoteProbe) Open() (*Player, error) {
	streamCtx, cancel := context.WithCancel(context.Background())
	s := &remoteSource{ctx: streamCtx, cancel: cancel, client: r.client, url: r.url, requests: make(map[uint64]context.CancelFunc)}
	h := cgo.NewHandle(s)
	raw := C.CString(r.kind)
	defer C.free(unsafe.Pointer(raw))
	p := &Player{native: C.hm_audio_remote(C.uintptr_t(h), C.longlong(r.total), raw), remote: s, handle: h}
	if p.native == nil {
		cancel()
		h.Delete()
		return nil, errors.New("native streaming player unavailable")
	}
	return p, nil
}

// OpenRemote probes the resource through the same allowlisted transport used by
// every subsequent range. AVFoundation only sees an application-private scheme.
// Callers on a background goroutine should use ProbeRemote + Open on the main
// thread instead.
func OpenRemote(ctx context.Context, url string) (*Player, error) {
	probe, err := ProbeRemote(ctx, url)
	if err != nil {
		return nil, err
	}
	return probe.Open()
}

//export hm_audio_read
func hm_audio_read(handle C.uintptr_t, request C.ulonglong, offset C.longlong, length C.int, out *unsafe.Pointer) C.int {
	s := cgo.Handle(handle).Value().(*remoteSource)
	ctx, cancel := context.WithCancel(s.ctx)
	s.mu.Lock()
	s.requests[uint64(request)] = cancel
	s.mu.Unlock()
	defer func() { cancel(); s.mu.Lock(); delete(s.requests, uint64(request)); s.mu.Unlock() }()
	chunk, err := s.client.Range(ctx, s.url, int64(offset), int(length))
	if err != nil {
		return -1
	}
	*out = C.CBytes(chunk.Bytes)
	return C.int(len(chunk.Bytes))
}

//export hm_audio_cancel_read
func hm_audio_cancel_read(handle C.uintptr_t, request C.ulonglong) {
	s := cgo.Handle(handle).Value().(*remoteSource)
	s.mu.Lock()
	defer s.mu.Unlock()
	if cancel := s.requests[uint64(request)]; cancel != nil {
		cancel()
	}
}
