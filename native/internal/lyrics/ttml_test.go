package lyrics

import (
	"errors"
	"strings"
	"testing"
)

const sampleTTML = `<?xml version="1.0" encoding="UTF-8"?>
<tt xmlns="http://www.w3.org/ns/ttml" xmlns:ttm="http://www.w3.org/ns/ttml#metadata" xml:lang="en">
  <head></head>
  <body>
    <div>
      <p begin="1.5s" end="4.0s"><span begin="1.5s" end="2.5s">Hello</span> <span begin="2.5s" end="4.0s">world</span></p>
      <p begin="5s" end="8s">Second line</p>
    </div>
  </body>
</tt>`

func TestParseTTMLWordsAndLines(t *testing.T) {
	lines, err := ParseTTML(sampleTTML)
	if err != nil {
		t.Fatalf("ParseTTML: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}
	if lines[0].Text != "Hello world" {
		t.Fatalf("text = %q", lines[0].Text)
	}
	if lines[0].Time != 1.5 {
		t.Fatalf("time = %v, want 1.5", lines[0].Time)
	}
	if len(lines[0].Words) != 2 || lines[0].Words[0].Text != "Hello" || lines[0].Words[1].Time != 2.5 {
		t.Fatalf("words = %+v", lines[0].Words)
	}
	if lines[1].Text != "Second line" || len(lines[1].Words) != 0 {
		t.Fatalf("line 2 = %+v", lines[1])
	}
}

func TestParseTTMLRelativeTimesUseParent(t *testing.T) {
	doc := `<tt xmlns="http://www.w3.org/ns/ttml"><body><div>
		<p begin="10s" dur="4s"><span begin="1s">one</span><span begin="2s">two</span></p>
	</div></body></tt>`
	lines, err := ParseTTML(doc)
	if err != nil {
		t.Fatalf("ParseTTML: %v", err)
	}
	if len(lines) != 1 {
		t.Fatalf("lines = %d", len(lines))
	}
	if got := lines[0].Words; len(got) != 2 || got[0].Time != 11 || got[1].Time != 12 {
		t.Fatalf("words = %+v", got)
	}
}

func TestParseTTMLClipsWordsToParentEnd(t *testing.T) {
	doc := `<tt xmlns="http://www.w3.org/ns/ttml"><body><div>
		<p begin="0s" end="3s"><span begin="2.5s" end="9s">late</span></p>
	</div></body></tt>`
	lines, err := ParseTTML(doc)
	if err != nil {
		t.Fatalf("ParseTTML: %v", err)
	}
	if len(lines) != 1 || len(lines[0].Words) != 1 {
		t.Fatalf("lines = %+v", lines)
	}
	if lines[0].Words[0].Time != 2.5 {
		t.Fatalf("word time = %v, want 2.5", lines[0].Words[0].Time)
	}
}

func TestParseTTMLRoles(t *testing.T) {
	doc := `<tt xmlns="http://www.w3.org/ns/ttml" xmlns:ttm="http://www.w3.org/ns/ttml#metadata"><body><div>
		<p begin="1s"><span>original</span><span ttm:role="translation">翻译</span><span ttm:role="transcription">transliteration</span></p>
	</div></body></tt>`
	lines, err := ParseTTML(doc)
	if err != nil {
		t.Fatalf("ParseTTML: %v", err)
	}
	if len(lines) != 1 {
		t.Fatalf("lines = %+v", lines)
	}
	if lines[0].Text != "original" || lines[0].Translation != "翻译" || lines[0].Romanization != "transliteration" {
		t.Fatalf("roles = %+v", lines[0])
	}
}

func TestParseTTMLRejectsSeqTiming(t *testing.T) {
	doc := `<tt xmlns="http://www.w3.org/ns/ttml"><body><div timeContainer="seq"><p begin="0s">x</p></div></body></tt>`
	if _, err := ParseTTML(doc); !errors.Is(err, ErrTTMLSeqTiming) {
		t.Fatalf("err = %v, want ErrTTMLSeqTiming", err)
	}
}

func TestParseTTMLRejectsOversize(t *testing.T) {
	if _, err := ParseTTML(strings.Repeat("a", maxTTMLBytes+1)); !errors.Is(err, ErrTTMLTooLarge) {
		t.Fatalf("err = %v, want ErrTTMLTooLarge", err)
	}
}

func TestParseTTMLRejectsMalformed(t *testing.T) {
	if _, err := ParseTTML("<tt><body><p>"); !errors.Is(err, ErrTTMLStructure) {
		t.Fatalf("err = %v, want ErrTTMLStructure", err)
	}
}

func TestParseTTMLSkipsEmptyLines(t *testing.T) {
	doc := `<tt xmlns="http://www.w3.org/ns/ttml"><body><div><p begin="1s"> </p><p begin="2s">real</p></div></body></tt>`
	lines, err := ParseTTML(doc)
	if err != nil {
		t.Fatalf("ParseTTML: %v", err)
	}
	if len(lines) != 1 || lines[0].Text != "real" {
		t.Fatalf("lines = %+v", lines)
	}
}

func TestParseAnyDetectsTTMLAndFallsBackToLRC(t *testing.T) {
	lines, format := ParseAny(sampleTTML)
	if format != "ttml" || len(lines) != 2 {
		t.Fatalf("format = %q lines = %d", format, len(lines))
	}
	lrc := "[00:01.00]hello\n[00:02.00]world"
	lines, format = ParseAny(lrc)
	if format != "lrc" || len(lines) != 2 {
		t.Fatalf("lrc format = %q lines = %d", format, len(lines))
	}
	if _, format := ParseAny("   "); format != "" {
		t.Fatalf("empty format = %q", format)
	}
}

func TestParseTTMLTimeUnits(t *testing.T) {
	cases := map[string]float64{
		"1.5s": 1.5, "500ms": 0.5, "01:02.500": 62.5, "1:00:00": 3600,
		"90f": 3, "10000000t": 1, "12": 12,
	}
	for raw, want := range cases {
		got, err := parseTTMLTime(raw)
		if err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		if diff := got - want; diff > 1e-6 || diff < -1e-6 {
			t.Fatalf("%s = %v, want %v", raw, got, want)
		}
	}
}
