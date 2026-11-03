package main

import (
	"fmt"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/ui"
	"math"
	"strings"
	"tinglan/native/internal/typography"
)

type typeState struct {
	centeredText   bool
	fontInk        map[string]ui.Rect
	fontFilter     flex.Field
	fontFilterView flex.View
	fontFilterText string
	fontCache      map[string]ui.Bitmap
	fontUsed       map[string]uint64
	fontTick       uint64
	fontID         uint64
	fontFamilies   []string
	fontIndex      int
	fontReveal     bool
	fontListOpen   bool
	fontScroll     float32
	fontBytes      int
	fontWidths     map[string]float32
}

func (m *model) textWidth(c *ui.Canvas, value string, size, weight float32) float32 {
	family := m.settings.Appearance.FontFamily
	if family == "" {
		family = typography.DefaultFamily()
	}
	size *= float32(m.settings.Appearance.FontScale)
	key := fmt.Sprintf("%s|%s|%g|%g", family, value, size, weight)
	if w, ok := m.fontWidths[key]; ok {
		return w
	}
	w := typography.Measure(value, family, size, weight)
	if w == 0 {
		w = c.TextWidthWeight(value, size, weight)
	}
	if m.fontWidths == nil || len(m.fontWidths) > 1024 {
		m.fontWidths = make(map[string]float32)
	}
	m.fontWidths[key] = w
	return w
}

var fontFilterID = tree.ID(shell, "font-filter")
var fontChoiceID = tree.ID(shell, "font-choice")
var fontItems = tree.Sub(shell, tree.Name("font-families"))

func (m *model) customText(c *ui.Canvas, value string, b ui.Rect, size, weight float32, tint ui.Color) bool {
	original := b
	family := m.settings.Appearance.FontFamily
	if family == "" {
		family = typography.DefaultFamily()
	}
	if family == "" {
		return false
	}
	// Rasterize glyph bounds, not the entire layout column; cache alpha in
	// discrete steps so lyric fading does not create a new bitmap every frame.
	tint.Alpha = float32(math.Round(float64(tint.Alpha)*16) / 16)
	glyphWidth := m.textWidth(c, value, size/float32(m.settings.Appearance.FontScale), weight) + 4
	b.Width = min(b.Width, glyphWidth)
	w, h := int(math.Ceil(float64(b.Width*2))), int(math.Ceil(float64(b.Height*2)))
	key := fmt.Sprintf("%s|%s|%g|%g|%d|%d|%v", family, value, size, weight, w, h, tint)
	bitmap, ok := m.fontCache[key]
	if !ok {
		bitmap = typography.Render(value, family, size, weight, w, h, tint)
		if bitmap.Width == 0 {
			return false
		}
		if m.fontCache == nil {
			m.fontCache = make(map[string]ui.Bitmap)
			m.fontInk = make(map[string]ui.Rect)
			m.fontUsed = make(map[string]uint64)
			m.fontBytes = 0
		}
		m.trimFontCache(len(bitmap.Pix))
		m.fontID++
		bitmap.ID = 100000 + m.fontID
		m.fontCache[key] = bitmap
		m.fontBytes += len(bitmap.Pix)
	}
	m.fontTick++
	m.fontUsed[key] = m.fontTick
	// Keep text semantics for accessibility/headless inspection while the
	// chosen family is drawn by CoreText into the native GPU bitmap layer.
	c.Label(value, b, ui.TextStyle{Size: size, Weight: weight, Color: ui.Color{}})
	if m.centeredText {
		ink, ok := m.fontInk[key]
		if !ok {
			left, top, right, bottom := bitmap.Width, bitmap.Height, 0, 0
			for y := 0; y < bitmap.Height; y++ {
				for x := 0; x < bitmap.Width; x++ {
					if bitmap.Pix[y*bitmap.Stride+x*4+3] > 0 {
						left = min(left, x)
						top = min(top, y)
						right = max(right, x+1)
						bottom = max(bottom, y+1)
					}
				}
			}
			ink = rect(float32(left), float32(top), float32(max(0, right-left)), float32(max(0, bottom-top)))
			if m.fontInk == nil {
				m.fontInk = make(map[string]ui.Rect)
			}
			m.fontInk[key] = ink
		}
		c.DrawBitmapRegion(bitmap, ink, rect(original.X+(original.Width-ink.Width/2)/2, original.Y+(original.Height-ink.Height/2)/2, ink.Width/2, ink.Height/2))
	} else {
		c.DrawBitmap(bitmap, b)
	}
	return true
}

const fontCacheBudget = 6 * 1024 * 1024

func (m *model) trimFontCache(incoming int) {
	for len(m.fontCache) > 0 && (len(m.fontCache) >= 256 || m.fontBytes+incoming > fontCacheBudget) {
		oldest, age := "", ^uint64(0)
		for key := range m.fontCache {
			if m.fontUsed[key] < age {
				oldest, age = key, m.fontUsed[key]
			}
		}
		m.fontBytes -= len(m.fontCache[oldest].Pix)
		delete(m.fontCache, oldest)
		delete(m.fontInk, oldest)
		delete(m.fontUsed, oldest)
	}
}
func (m *model) paintFontList(c *ui.Canvas, size ui.Size) {
	modalFocus(c, fontDialogScope, fontFilterID, closePanelID)
	defer c.PopFocusScope()
	c.PushLayer(4)
	defer c.PopLayer()
	c.Fill(rect(0, 0, size.Width, size.Height), color(0x16181c, .35))
	c.Block(rect(0, 0, size.Width, size.Height))
	b := rect(size.Width/2-230, 70, 460, size.Height-140)
	m.panel(c, b, 16, 1)
	m.text(c, "选择已安装字体", rect(b.X+20, b.Y+18, 340, 30), 20, 600, m.ink())
	if m.pill(c, closePanelID, rect(b.X+b.Width-80, b.Y+18, 60, 30), "关闭", false) {
		m.fontListOpen = false
	}
	submitted := false
	m.fontFilterView.Draw(c, m.fieldTheme(), rect(b.X+20, b.Y+60, b.Width-40, 36), func(f *flex.Frame) {
		submitted = f.Field(&m.fontFilter, fontFilterID, "搜索系统字体", "输入字体名称筛选").Fill().Submitted()
	})
	if m.fontFilterText != m.fontFilter.Text() {
		m.fontFilterText = m.fontFilter.Text()
		m.fontScroll = 0
		m.fontIndex = 0
	}
	names := m.filteredFonts()
	if submitted && len(names) > 0 {
		m.chooseFont(names[min(m.fontIndex, len(names)-1)])
	}
	if m.fontReveal && len(names) > 0 {
		m.fontReveal = false
		top := float32(m.fontIndex) * 36
		m.fontScroll = max(min(m.fontScroll, top), top+36-(b.Height-124))
	}
	if len(names) == 0 {
		m.text(c, "没有匹配的系统字体", rect(b.X+20, b.Y+110, b.Width-40, 30), 13, 400, m.muted())
	}
	c.Clip(rect(b.X+16, b.Y+108, b.Width-32, b.Height-124))
	for i, name := range names {
		y := b.Y + 108 + float32(i)*36 - m.fontScroll
		if y < b.Y+74 || y > b.Y+b.Height {
			continue
		}
		r := rect(b.X+20, y, b.Width-40, 34)
		if i == m.fontIndex {
			tint := m.accent()
			tint.Alpha = .1
			c.Rounded(r, 6, tint)
		}
		m.text(c, name, rect(r.X+8, r.Y, r.Width-16, r.Height), 13, 400, m.ink())
		if hit(c, fontItems.At(i), r, name) {
			m.chooseFont(name)
		}
	}
	c.Unclip()
}

func (m *model) filteredFonts() []string {
	query := strings.ToLower(strings.TrimSpace(m.fontFilter.Text()))
	var result []string
	for _, name := range append([]string{"默认字体"}, m.fontFamilies...) {
		if strings.Contains(strings.ToLower(name), query) {
			result = append(result, name)
		}
	}
	return result
}
