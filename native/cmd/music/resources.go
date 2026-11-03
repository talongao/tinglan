package main

import (
	"bytes"
	"context"
	_ "golang.org/x/image/webp"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"math"
	"os"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
	"tinglan/native/internal/lyrics"
	"tinglan/native/internal/music"
	"tinglan/native/internal/resource"
	"tinglan/native/internal/source"
	"tinglan/native/internal/storage"
	"tinglan/native/internal/visual"
)

type coverResult struct {
	resolved string
	epoch    uint64
	url      string
	bitmap   ui.Bitmap
	stage    ui.Bitmap
}
type resourceState struct {
	resourceClient   *resource.Client
	coverRetryTimer  *time.Timer
	coverAlternates  map[string][]string
	coverRetry       map[string]time.Time
	coverUsed        map[string]uint64
	coverTick        uint64
	albumCovers      map[string]string
	brandTint        ui.Color
	brandBitmap      ui.Bitmap
	glassKey         uint64
	glassID          uint64
	glassBitmap      ui.Bitmap
	entityGeneration uint64
	entityResults    []entityResult
	stageCovers      map[string]ui.Bitmap
	covers           map[string]ui.Bitmap
	coverPending     map[string]bool
	coverResults     []coverResult
	coverID          uint64
	lyricSource      string
	entity           *music.Detail
	entityKind       string
	entityBack       int
	entityAlbumPage  int
}

var importLyricsID = tree.ID(shell, "import-lyrics")
var removeLyricsID = tree.ID(shell, "remove-lyrics")
var clearLyricsID = tree.ID(shell, "clear-lyrics")

func (m *model) saveLyricOffset() {
	if track := m.queue.Current(); track != nil && m.store != nil {
		if err := m.store.SaveLyricOffset(m.trackOffsetKey(*track), m.lyricOffset); err != nil {
			m.setStatus("歌词偏移保存失败")
		}
	}
}

func (m *model) removeLyrics() {
	track := m.queue.Current()
	if track == nil || m.store == nil {
		return
	}
	if err := m.store.RemoveLocalLyrics(track.ID); err != nil {
		m.setStatus("移除歌词失败")
		return
	}
	m.lyricLines = nil
	m.lyricSource = ""
	if doc, err := m.store.LoadLyrics(track.ID); err == nil {
		m.lyricLines, _ = lyrics.ParseAny(doc.Text)
		m.lyricSource = doc.Source
	}
	m.setStatus("已移除本曲导入歌词")
}

func (m *model) cover(c *ui.Canvas, raw string, b ui.Rect) bool {
	if raw == "" || !m.resourceAllowed(raw) {
		return false
	}
	if bitmap, ok := m.covers[raw]; ok {
		m.touchCover(raw)
		if bitmap.Width > 0 {
			// Each simultaneous presentation gets its own native texture identity.
			if b.Width > 100 {
				bitmap.ID += 1 << 40
			}
			// Viva 0.1.3's macOS texture hash includes destination size, while
			// its upload check does not. A resize must upload pixels again.
			bitmap.Version = coverPresentationVersion(b)
			c.DrawBitmap(bitmap, b)
			return true
		}
		return false
	}
	if m.coverPending == nil {
		m.coverPending = make(map[string]bool)
	}
	if m.demo || m.coverPending[raw] || len(m.coverPending) >= 2 || time.Now().Before(m.coverRetry[raw]) {
		return false
	}
	m.coverPending[raw] = true
	epoch := m.accountEpoch
	store := m.store
	candidates := append([]string{raw}, m.coverAlternates[raw]...)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		var bitmap ui.Bitmap
		var stage ui.Bitmap
		for _, candidate := range candidates {
			if !m.resourceAllowed(candidate) {
				continue
			}
			var data []byte
			var err error
			cached := false
			if store != nil {
				data, err = store.LoadCover(candidate)
				cached = err == nil
			}
			if cached {
				if _, _, decodeErr := image.DecodeConfig(bytes.NewReader(data)); decodeErr != nil {
					cached = false
				}
			}
			if !cached {
				data, err = resource.New().Bytes(ctx, candidate, 8*1024*1024)
			}
			if err == nil {
				if config, _, err := image.DecodeConfig(bytes.NewReader(data)); err == nil && config.Width > 0 && config.Height > 0 && config.Width <= 4096 && config.Height <= 4096 {
					if img, _, err := image.Decode(bytes.NewReader(data)); err == nil {
						w, h := 256, 256
						pix := make([]byte, w*h*4)
						bounds := img.Bounds()
						side := min(config.Width, config.Height)
						ox, oy := bounds.Min.X+(config.Width-side)/2, bounds.Min.Y+(config.Height-side)/2
						for y := 0; y < h; y++ {
							for x := 0; x < w; x++ {
								r, g, b, a := img.At(ox+x*side/w, oy+y*side/h).RGBA()
								at := (y*w + x) * 4
								pix[at], pix[at+1], pix[at+2], pix[at+3] = uint8(b>>8), uint8(g>>8), uint8(r>>8), uint8(a>>8)
							}
						}
						if store != nil && !cached {
							small := image.NewNRGBA(image.Rect(0, 0, w, h))
							for i := 0; i < len(pix); i += 4 {
								small.Pix[i], small.Pix[i+1], small.Pix[i+2], small.Pix[i+3] = pix[i+2], pix[i+1], pix[i], pix[i+3]
							}
							var encoded bytes.Buffer
							if png.Encode(&encoded, small) == nil {
								_ = store.SaveCover(candidate, encoded.Bytes())
							}
						}
						bitmap = ui.Bitmap{Pix: pix, Width: w, Height: h, Stride: w * 4, Version: 1}
						// Stage shares the application background; no blurred cover copy.
						bitmap = visual.Round(bitmap, 22)
					}
				}
			}
			if bitmap.Width > 0 {
				break
			}
		}
		m.mu.Lock()
		if !m.closed {
			m.coverResults = append(m.coverResults, coverResult{epoch: epoch, url: raw, bitmap: bitmap, stage: stage})
		}
		m.mu.Unlock()
		m.requestUpdate()
	}()
	return false
}

func coverPresentationVersion(b ui.Rect) uint32 {
	w := uint32(max(1, min(65535, math.Round(float64(b.Width)))))
	h := uint32(max(1, min(65535, math.Round(float64(b.Height)))))
	return w<<16 | h
}
func (m *model) applyCovers() {
	defer func() {
		if m.coverRetryTimer != nil {
			m.coverRetryTimer.Stop()
			m.coverRetryTimer = nil
		}
		var next time.Time
		now := time.Now()
		for _, at := range m.coverRetry {
			if at.After(now) && (next.IsZero() || at.Before(next)) {
				next = at
			}
		}
		if !next.IsZero() {
			m.coverRetryTimer = time.AfterFunc(time.Until(next)+time.Millisecond, m.requestUpdate)
		}
	}()
	m.mu.Lock()
	results := m.coverResults
	m.coverResults = nil
	m.mu.Unlock()
	for _, r := range results {
		if r.epoch != m.accountEpoch {
			continue
		}
		delete(m.coverPending, r.url)
		if r.resolved != "" {
			delete(m.coverRetry, r.url)
			if m.albumCovers == nil {
				m.albumCovers = make(map[string]string)
			}
			m.albumCovers[r.url] = r.resolved
			continue
		}
		if r.bitmap.Width == 0 {
			if m.coverRetry == nil {
				m.coverRetry = make(map[string]time.Time)
			}
			m.coverRetry[r.url] = time.Now().Add(30 * time.Second)
			continue
		}
		m.coverID++
		r.bitmap.ID = 100 + m.coverID
		if m.covers == nil {
			m.covers = make(map[string]ui.Bitmap)
		}
		if len(m.covers) >= 16 {
			old := ""
			age := ^uint64(0)
			for key := range m.covers {
				if m.coverUsed[key] < age {
					old, age = key, m.coverUsed[key]
				}
			}
			delete(m.covers, old)
			delete(m.stageCovers, old)
			delete(m.coverUsed, old)
		}
		delete(m.coverRetry, r.url)
		m.touchCover(r.url)
		m.covers[r.url] = r.bitmap
		if m.stageCovers == nil {
			m.stageCovers = make(map[string]ui.Bitmap)
		}
		r.stage.ID = 1000000 + m.coverID
		m.stageCovers[r.url] = r.stage
	}
}
func (m *model) importLyrics() {
	track := m.queue.Current()
	if track == nil {
		return
	}
	paths, err := app.OpenFiles(app.OpenFileOptions{Title: "导入歌词"})
	if err != nil || len(paths) == 0 {
		return
	}
	info, err := os.Stat(paths[0])
	if err != nil || !info.Mode().IsRegular() || info.Size() > 512*1024 {
		m.setStatus("歌词文件不可用或超过 512KB")
		return
	}
	data, err := os.ReadFile(paths[0])
	if err != nil {
		return
	}
	text, err := lyrics.Decode(data)
	if err != nil {
		m.setStatus("歌词编码无法识别")
		return
	}
	lines, format := lyrics.ParseAny(text)
	if len(lines) == 0 {
		m.setStatus("歌词文件没有可用内容")
		return
	}
	if format == "ttml" {
		m.setStatus("已导入 TTML 逐字歌词")
	}
	if m.store != nil {
		if err := m.store.SaveLyrics(track.ID, storage.LyricDocument{Text: text, Source: "本地导入"}, true); err != nil {
			m.setStatus("歌词保存失败")
			return
		}
	}
	m.lyricLines = lines
	m.lyricSource = "本地导入"
}

func (m *model) touchCover(raw string) {
	if m.coverUsed == nil {
		m.coverUsed = make(map[string]uint64)
	}
	m.coverTick++
	m.coverUsed[raw] = m.coverTick
}

func (m *model) trackCover(c *ui.Canvas, t music.Track, b ui.Rect) bool {
	if t.CoverURL != "" {
		if m.coverAlternates == nil {
			m.coverAlternates = make(map[string][]string)
		}
		m.coverAlternates[t.CoverURL] = t.CoverURLs
		if m.cover(c, t.CoverURL, b) {
			return true
		}
		if m.coverRetry[t.CoverURL].IsZero() {
			return false
		}
	}
	if t.Album == nil || t.Album.ID == "" || m.demo {
		return false
	}
	key := "album:" + t.Album.ID
	if raw := m.albumCovers[key]; raw != "" {
		return m.cover(c, raw, b)
	}
	if m.coverPending == nil {
		m.coverPending = make(map[string]bool)
	}
	if m.coverPending[key] || len(m.coverPending) >= 2 || time.Now().Before(m.coverRetry[key]) {
		return false
	}
	m.coverPending[key] = true
	epoch, id := m.accountEpoch, t.Album.ID
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		detail, err := m.catalogSource().AlbumDetail(ctx, source.EntityRequest{Ref: source.EntityRef{Source: music.SourceHuawei, Kind: "album", ID: id}, Limit: 1})
		raw := detail.Album.CoverURL
		if raw == "" && len(detail.Tracks) > 0 {
			raw = detail.Tracks[0].CoverURL
		}
		if err != nil {
			raw = ""
		}
		m.mu.Lock()
		if !m.closed {
			m.coverResults = append(m.coverResults, coverResult{epoch: epoch, url: key, resolved: raw})
		}
		m.mu.Unlock()
		m.requestUpdate()
	}()
	return false
}

// resourceAllowed applies the allowlist declared by the built-in source, so
// media and cover URLs never depend on a provider hard-coded in the shell.
func (m *model) resourceAllowed(raw string) bool {
	if m.resourceClient == nil {
		return false
	}
	return m.resourceClient.Allows(raw)
}

// resourceClient is built once per adapter descriptor so media URLs follow the
// same allowlist the adapter declares.
func (m *model) initResourceClient() {
	domains := []string{".dbankcdn.cn", ".tingmall.com", ".kuke.com"}
	if m.builtinSource != nil {
		domains = m.builtinSource.Describe().ResourceDomains
	}
	m.resourceClient = resource.NewFor(domains)
}
