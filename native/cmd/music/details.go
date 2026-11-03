package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/vivaui/viva/ui"
	"tinglan/native/internal/huawei"
	"tinglan/native/internal/icons"
	"tinglan/native/internal/library"
	"tinglan/native/internal/visual"
)

var albumRows = tree.Sub(shell, tree.Name("album-links"))
var artistRows = tree.Sub(shell, tree.Name("artist-links"))
var entityAlbums = tree.Sub(shell, tree.Name("artist-albums"))
var entityAlbumsPrevious = tree.ID(shell, "artist-albums-previous")
var entityAlbumsNext = tree.ID(shell, "artist-albums-next")

type entityResult struct {
	generation uint64
	detail     huawei.Detail
	err        error
}

func (m *model) openEntity(kind, id, title string) {
	if id == "" {
		return
	}
	m.entityGeneration++
	gen := m.entityGeneration
	m.entity = &huawei.Detail{ID: id, Title: title}
	m.entityKind = kind
	m.entityScroll = 0
	m.entityAlbumPage = 0
	if m.demo {
		m.entity.Tracks = syntheticTracks(min(m.demoCount, 20))
		return
	}
	m.setStatus("正在读取详情…")
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		client := huawei.NewClient()
		r := entityResult{generation: gen}
		if kind == "artist" {
			r.detail, r.err = client.ArtistDetail(ctx, id)
		} else {
			catalog := library.New(func(ctx context.Context, start, limit int) (huawei.Page, error) {
				d, err := client.AlbumDetail(ctx, id, start, limit)
				if start == 0 {
					r.detail = d
				}
				return huawei.Page{Tracks: d.Tracks, More: d.More, Next: d.Next, RawCount: d.Next - start}, err
			})
			r.err = catalog.LoadAll(ctx)
			r.detail.Tracks, _ = catalog.Snapshot()
		}
		m.mu.Lock()
		if !m.closed {
			m.entityResults = append(m.entityResults, r)
		}
		m.mu.Unlock()
		m.requestUpdate()
	}()
}
func (m *model) applyEntities() {
	m.mu.Lock()
	results := m.entityResults
	m.entityResults = nil
	m.mu.Unlock()
	for _, r := range results {
		if r.generation != m.entityGeneration || m.entity == nil {
			continue
		}
		if r.err != nil {
			if m.expireSession(r.err) {
				continue
			}
			m.setStatus("详情读取失败，请返回后重试")
			continue
		}
		detail := r.detail
		// Album responses often omit the title even though the row that opened
		// them carried one; keep the entry label instead of rendering a blank.
		if strings.TrimSpace(detail.Title) == "" {
			detail.Title = m.entity.Title
		}
		if detail.CoverURL == "" {
			detail.CoverURL = m.entity.CoverURL
		}
		m.entity = &detail
	}
}
func (m *model) paintEntity(c *ui.Canvas, b ui.Rect) {
	if m.pill(c, backID, rect(b.X+b.Width-90, b.Y-40, 90, 30), "返回", false) {
		m.entity = nil
		m.entityGeneration++
		return
	}
	kind := "专辑"
	if m.entityKind == "artist" {
		kind = "歌手"
	}
	meta := fmt.Sprintf("%d 首", len(m.entity.Tracks))
	if m.entityKind == "album" && len(m.entity.Tracks) > 0 {
		meta = m.entity.Tracks[0].ArtistText() + " · " + meta
	}
	// The whole page scrolls: cover, artist albums and track list move
	// together. Scrolling only the list left a 4-row window glued under the
	// cover, which read as "the album page cannot scroll".
	contentH := entityContentHeight(b.Width, b.Height, len(m.entity.Albums), len(m.entity.Tracks))
	off := max(0, min(max(0, contentH-b.Height), m.entityScroll))
	m.entityScroll = off
	c.Clip(b)
	hero := rect(b.X, b.Y-off, b.Width, b.Height)
	listY := m.detailHero(c, hero, kind, "", m.entity.CoverURL, meta, m.entity.Tracks)
	if len(m.entity.Albums) > 0 {
		columns := max(1, int((b.Width-86)/180))
		start := min(m.entityAlbumPage*columns, len(m.entity.Albums)-1)
		for i := start; i < min(start+columns, len(m.entity.Albums)); i++ {
			a := m.entity.Albums[i]
			if m.pill(c, entityAlbums.At(i), rect(b.X+21+float32(i-start)*180, listY, 168, 32), a.Title, false) {
				m.openEntity("album", a.ID, a.Title)
				c.Unclip()
				return
			}
		}
		if start > 0 && m.pill(c, entityAlbumsPrevious, rect(b.X+b.Width-80, listY, 36, 32), "<", false) {
			m.entityAlbumPage--
		}
		if start+columns < len(m.entity.Albums) && m.pill(c, entityAlbumsNext, rect(b.X+b.Width-38, listY, 36, 32), ">", false) {
			m.entityAlbumPage++
		}
		listY += 44
	}
	saved := m.entityScroll
	m.entityScroll = 0
	m.paintTrackList(c, rect(b.X+21, listY, b.Width-42, contentH), m.entity.Tracks, true, true)
	m.entityScroll = saved
	c.Unclip()
}

// entityContentHeight matches what paintEntity paints so the scroll limit and
// the painting agree; otherwise one of them leaves blank space or hides rows.
func entityContentHeight(w, h float32, albums, tracks int) float32 {
	side := float32(150)
	if w < 800 {
		side = 112
	}
	if h < 300 {
		side = 84
	}
	total := 15 + side + 18 + float32(tracks)*58 + 24
	if albums > 0 {
		total += 44
	}
	return total
}

func (m *model) detailHero(c *ui.Canvas, b ui.Rect, kind, title, cover, meta string, tracks []huawei.Track) float32 {
	side, gap := float32(150), float32(24)
	if b.Width < 800 {
		side = 112
		gap = 18
	}
	// A 900×600 window leaves ~320pt of content: the full hero then pushes the
	// track list to two visible rows, so shrink it instead of hiding the list.
	short := b.Height < 300
	if short {
		side, gap = 84, 14
	}
	art := rect(b.X+23, b.Y+15, side, side)
	c.Shadow(art, 16, ui.ShadowStyle{OffsetY: 16, Blur: 36}, color(0x101216, .14))
	visual.RoundedGradient(c, art, 16, color(0xead9d6, 1), color(0xcbb8b3, 1))
	icon := icons.ListMusic
	if kind == "专辑" {
		icon = icons.Disc3
	}
	c.DrawIcon(icon, rect(art.X+side/2-17, art.Y+side/2-17, 34, 34), color(0xffffff, .92))
	m.cover(c, cover, art)
	x := art.X + side + gap
	w := b.X + b.Width - 23 - x
	y := art.Y + side - 126
	titleSize := float32(26)
	if short {
		y = art.Y + 4
		titleSize = 18
	}
	m.text(c, kind, rect(x, y, w, 16), 10, 700, m.muted())
	// The page header already shows the entity name; repeating it in the hero
	// only pushed the track list down.
	metaY, playY := float32(62), float32(90)
	if title != "" {
		m.text(c, title, rect(x, y+23, w, 32), titleSize, 680, m.ink())
	} else {
		metaY, playY = 23, 52
	}
	m.text(c, meta, rect(x, y+metaY, w, 18), 11, 400, m.muted())
	play := rect(x, y+playY, 98, 32)
	c.Shadow(play, 16, ui.ShadowStyle{OffsetY: 6, Blur: 14}, color(0xc8202e, .2))
	c.Rounded(play, 16, m.accent())
	c.DrawIcon(icons.PlayFilled, rect(play.X+16, play.Y+10, 12, 12), color(0xffffff, 1))
	m.text(c, "播放全部", rect(play.X+35, play.Y, 58, 32), 12, 600, color(0xffffff, 1))
	if hit(c, allID, play, "播放全部") {
		m.startTracks(tracks, 0)
	}
	return art.Y + side + 18
}
