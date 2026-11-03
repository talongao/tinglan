package main

import (
	"github.com/vivaui/viva/ui"
	"tinglan/native/internal/visual"
)

func (m *model) glass(c *ui.Canvas, b ui.Rect, radius, opacity float32) {
	if m.palette().dark {
		m.panel(c, b, radius, 1)
		return
	}
	key := visual.BackdropKey(c, b, radius, opacity)
	if key != m.glassKey {
		bitmap, _ := visual.Backdrop(c, b, 22, radius, opacity)
		m.glassID++
		bitmap.ID = 900000 + m.glassID
		m.glassBitmap = bitmap
		m.glassKey = key
	}
	c.Shadow(b, radius, ui.ShadowStyle{OffsetY: 14, Blur: 40}, color(0x101216, .07))
	c.DrawBitmap(m.glassBitmap, b)
}

func (m *model) backgroundPanel(c *ui.Canvas, b ui.Rect, radius, opacity float32) {
	m.panel(c, b, radius, opacity)
}
