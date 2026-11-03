package main

import (
	"context"
	"errors"
	"log"
	"math"
	"time"

	"tinglan/native/internal/audio"
	"tinglan/native/internal/lyrics"
	"tinglan/native/internal/mainthread"
	"tinglan/native/internal/music"
	"tinglan/native/internal/playback"
	"tinglan/native/internal/resource"
	"tinglan/native/internal/storage"
)

type cachedResolve struct {
	playback music.Playback
	link     music.SourceLink
	at       time.Time
}

type playbackResult struct {
	generation  uint64
	player      *audio.Player
	playback    music.Playback
	link        music.SourceLink
	lyrics      []lyrics.Line
	lyricSource string
	err         error
	lyricsOnly  bool
}

func lyricsCacheKey(link music.SourceLink) string {
	return link.Source.String() + ":" + link.TrackID
}

func (m *model) resolveCurrent() {
	track := m.queue.Current()
	if track == nil {
		return
	}
	if m.playbackCancel != nil {
		m.playbackCancel()
	}
	m.playbackGeneration++
	generation := m.playbackGeneration
	ctx, cancel := context.WithCancel(context.Background())
	m.playbackCancel = cancel
	m.resolving = true
	quality := m.settings.Quality
	store := m.store
	fallbackEnabled := m.settings.ThirdPartyLyrics
	query := lyrics.Query{Title: track.Title, Artist: track.ArtistText()}
	if track.Duration != nil {
		query.Duration = *track.Duration
	}
	row, ok := m.canonicalFor(*track)
	// Resolving is visible in the player bar; do not raise a toast for it.
	go func() {
		r := playbackResult{generation: generation}
		if !ok || m.resolver == nil {
			r.err = errors.New("no playable source")
		} else {
			var resolved playback.Result
			cacheKey := row.ID + "|" + quality
			m.resolveMu.Lock()
			cached, hit := m.resolveCache[cacheKey]
			m.resolveMu.Unlock()
			if hit && time.Since(cached.at) < 5*time.Minute && cached.playback.URL != "" {
				resolved = playback.Result{Playback: cached.playback, Link: cached.link, Access: music.AccessFull}
			} else {
				resolved, r.err = m.resolver.Resolve(ctx, row, quality)
				if r.err == nil {
					m.resolveMu.Lock()
					if m.resolveCache == nil {
						m.resolveCache = make(map[string]cachedResolve)
					}
					m.resolveCache[cacheKey] = cachedResolve{playback: resolved.Playback, link: resolved.Link, at: time.Now()}
					m.resolveMu.Unlock()
				}
			}
			r.playback = resolved.Playback
			r.link = resolved.Link
			if r.err == nil {
				r.player, r.err = openRemote(ctx, r.playback.URL)
			}
		}
		if r.err != nil {
			cancel()
			m.postPlayback(r)
			return
		}
		// Start playback as soon as the stream is open: lyrics must never sit
		// on the critical path (the third-party fallback can take seconds).
		text := r.playback.LyricText
		r.lyricSource = sourceLabel(r.link.Source.String())
		key := lyricsCacheKey(r.link)
		cached := false
		if store != nil {
			if doc, err := store.LoadLyrics(key); err == nil {
				text, r.lyricSource, cached = doc.Text, doc.Source, true
			}
		}
		if text != "" {
			r.lyrics, _ = lyrics.ParseAny(text)
		}
		m.postPlayback(r)
		if !cached && text == "" && r.playback.LyricURL != "" {
			if data, err := resource.New().Bytes(ctx, r.playback.LyricURL, 512*1024); err == nil {
				text, _ = lyrics.Decode(data)
			}
		}
		if fallbackEnabled && len(r.lyrics) == 0 && ctx.Err() == nil && text == "" {
			text, r.lyricSource = lyrics.Fallback(ctx, query)
		}
		if text != "" {
			r.lyrics, _ = lyrics.ParseAny(text)
		}
		if !cached && len(r.lyrics) > 0 && store != nil {
			_ = store.SaveLyrics(key, storage.LyricDocument{Text: text, Source: r.lyricSource}, false)
		}
		if len(r.lyrics) > 0 {
			m.postPlayback(playbackResult{generation: generation, lyricsOnly: true, lyrics: r.lyrics, lyricSource: r.lyricSource})
		}
	}()
}

// openRemote keeps the blocking HTTP probe off the main thread and creates the
// AVPlayer item on it. AVFoundation only reports readiness for items created on
// a thread with a running run loop; a worker-created item stays at status
// unknown with a zero duration and the player bar never leaves 0:00.
func openRemote(ctx context.Context, url string) (*audio.Player, error) {
	probe, err := audio.ProbeRemote(ctx, url)
	if err != nil {
		return nil, err
	}
	type opened struct {
		player *audio.Player
		err    error
	}
	result := make(chan opened, 1)
	mainthread.Post(func() {
		player, err := probe.Open()
		result <- opened{player: player, err: err}
	})
	select {
	case o := <-result:
		return o.player, o.err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(5 * time.Second):
		// The main queue is not being serviced (headless tests); fall back to
		// creating the item here so the failure surfaces as a playback error
		// instead of blocking the resolver.
		return probe.Open()
	}
}

func (m *model) postPlayback(r playbackResult) {
	m.mu.Lock()
	if !m.closed {
		m.playbackResults = append(m.playbackResults, r)
	} else if r.player != nil {
		r.player.Close()
	}
	m.mu.Unlock()
	m.requestUpdate()
}

func (m *model) applyPlaybackResults() {
	m.mu.Lock()
	results := m.playbackResults
	m.playbackResults = nil
	m.mu.Unlock()
	for _, r := range results {
		if r.generation != m.playbackGeneration {
			if r.player != nil {
				r.player.Close()
			}
			continue
		}
		if r.lyricsOnly {
			// A local import made while a provider request was in flight wins.
			if m.lyricSource != "本地导入" {
				m.lyricLines = r.lyrics
				m.lyricSource = r.lyricSource
			}
			m.playbackCancel = nil
			continue
		}
		m.resolving = false
		if r.err != nil {
			if m.expireSession(r.err) {
				continue
			}
			if errors.Is(r.err, playback.ErrNoSource) {
				m.playbackError = "没有可用音源"
				m.setStatus("没有可用音源：请确认已登录，或该曲目在导入平台上仍可播放。")
			} else {
				m.playbackError = "播放失败"
				m.setStatus("播放失败，请重试或选择其他音质。")
			}
			// Leave the transport in a state the user can act on: a paused
			// icon with 0:00 looks like a stuck player, not a failure.
			m.audioRunning = false
			m.audioState = audio.State{}
			m.startedAt = time.Time{}
			m.pendingSeek = 0
			if r.player != nil {
				r.player.Close()
			}
			continue
		}
		m.player = r.player
		m.playbackError = ""
		m.lyricLines = r.lyrics
		m.lyricSource = r.lyricSource
		m.lyricTrack = ""
		m.actualQuality = r.playback.Quality
		m.currentAccess = r.playback.Access
		m.playedLink = r.link
		if r.playback.Duration != nil && *r.playback.Duration > 0 {
			m.resolvedDuration = *r.playback.Duration
		}
		_ = m.player.SetVolume(m.settings.Volume)
		m.player.Play()
		m.audioRunning = true
		m.startedAt = time.Now()
		if r.playback.Access == music.AccessPreview {
			m.setStatus("所有音源都只提供试听，正在播放试听片段。")
		} else {
			m.setStatus("")
		}
		m.pollAudio()
	}
}

func (m *model) cancelPlayback() {
	m.playbackGeneration++
	m.resolving = false
	m.pendingSeek = 0
	if m.playbackCancel != nil {
		m.playbackCancel()
		m.playbackCancel = nil
	}
	if m.playTimer != nil {
		m.playTimer.Stop()
		m.playTimer = nil
	}
	if m.player != nil {
		m.player.Close()
		m.player = nil
	}
	m.audioRunning = false
}

func (m *model) seekPlayback(seconds float64) {
	if m.player == nil || math.IsNaN(seconds) || math.IsInf(seconds, 0) {
		return
	}
	state := m.player.Snapshot()
	if !state.Ready || state.Duration <= 0 {
		return
	}
	seconds = max(0, min(seconds, state.Duration))
	if m.player.Seek(seconds) != nil {
		return
	}
	m.pendingSeek = 0
	m.audioState = state
	m.audioState.Position = seconds
	m.queue.Position = seconds
	m.lastPosition = seconds
	m.lastPoll = time.Now()
	m.updateMedia()
	m.requestUpdate()
}

// Only numeric durations are logged: never track identifiers or signed URLs.
func (m *model) checkDuration() {
	if m.durationMismatch || !m.audioState.Ready || m.audioState.Duration <= 0 {
		return
	}
	catalog := 0.0
	if t := m.queue.Current(); t != nil && t.Duration != nil {
		catalog = *t.Duration
	}
	media := m.audioState.Duration
	if (catalog > 0 && math.Abs(catalog-media) > 2) || (m.resolvedDuration > 0 && math.Abs(m.resolvedDuration-media) > 2) {
		m.durationMismatch = true
		log.Printf("playback duration mismatch: catalog=%.3fs resolved=%.3fs audio=%.3fs; using audio duration", catalog, m.resolvedDuration, media)
	}
}
