package main

import (
	"testing"

	"github.com/vivaui/viva/ui"
	"tinglan/native/internal/lyrics"
)

func TestSungWidthAdvances(t *testing.T) {
	m := &model{}
	m.lyricLines = []lyrics.Line{{Time: 10, Text: "abcde", Words: []lyrics.Word{
		{Time: 10, Text: "a"}, {Time: 11, Text: "b"}, {Time: 12, Text: "c"}, {Time: 13, Text: "d"}, {Time: 14, Text: "e"},
	}}, {Time: 20, Text: "next"}}
	var c ui.Canvas
	cp := &c
	w0 := m.sungWidth(cp, m.lyricLines[0], 0, 10, 20)
	w1 := m.sungWidth(cp, m.lyricLines[0], 0, 10.5, 20)
	w2 := m.sungWidth(cp, m.lyricLines[0], 0, 11.5, 20)
	full := m.sungWidth(cp, m.lyricLines[0], 0, 19, 20)
	if !(w0 < w1 && w1 < w2 && w2 < full) {
		t.Fatalf("widths %v %v %v %v", w0, w1, w2, full)
	}
}
