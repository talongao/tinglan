package lyrics

import "testing"

func TestMergeSubtitlesKeepsWordsAndFillsTranslation(t *testing.T) {
	lines := []Line{
		{Time: 1, Text: "hello world", Words: []Word{{Time: 1, Text: "hello"}, {Time: 1.4, Text: "world"}}},
		{Time: 5, Text: "second line"},
	}
	extra := []Line{
		{Time: 1.05, Text: "hello world", Translation: "你好世界", Romanization: "hello world"},
		{Time: 5.2, Text: "other text", Translation: "第二行"},
	}
	got := MergeSubtitles(lines, extra)
	if len(got[0].Words) != 2 {
		t.Fatalf("words lost: %+v", got[0])
	}
	if got[0].Translation != "你好世界" || got[0].Romanization != "hello world" {
		t.Fatalf("first line = %+v", got[0])
	}
	if got[1].Translation != "第二行" {
		t.Fatalf("second line = %+v", got[1])
	}
	if got[1].Text != "second line" {
		t.Fatalf("plain text must not override different words: %q", got[1].Text)
	}
}

func TestHasSubtitles(t *testing.T) {
	if HasSubtitles([]Line{{Text: "a"}}) {
		t.Fatal("expected no subtitles")
	}
	if !HasSubtitles([]Line{{Text: "a", Romanization: "b"}}) {
		t.Fatal("expected subtitles")
	}
}

func TestParseDropsCreditBlockFromSubtitles(t *testing.T) {
	raw := "[00:00.00]晴天 - 周杰伦\n[00:00.00]词：陈曦\n曲：董冬冬\n制作人：董冬冬\n[00:12.00]故事的小黄花"
	lines := Parse(raw)
	if len(lines) == 0 {
		t.Fatal("no lines")
	}
	if got := lines[0].Translation; got != "" {
		t.Fatalf("credit block leaked into translation: %q", got)
	}
}
