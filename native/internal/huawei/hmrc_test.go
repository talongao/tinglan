package huawei

import (
	"testing"

	"tinglan/native/internal/lyrics"
)

func TestParseHMRCBuildsWordTimings(t *testing.T) {
	raw := "\ufeff[ti:测试]\n[ar:测试歌手]\n[0,2582]<0,287>晴<287,287>天<574,287><861,287>周\r\n[2582,2000]<0,500>第<500,500>二行\r\n"
	lines := parseHMRC(raw)
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}
	first := lines[0]
	if first.Time != 0 || first.Text != "晴天周" {
		t.Fatalf("first = %+v", first)
	}
	if len(first.Words) != 3 {
		t.Fatalf("words = %d, want 3", len(first.Words))
	}
	if first.Words[0].Time != 0 || first.Words[1].Time != 0.287 || first.Words[2].Time != 0.861 {
		t.Fatalf("word times = %+v", first.Words)
	}
	if lines[1].Time != 2.582 || lines[1].Words[1].Time != 3.082 {
		t.Fatalf("second = %+v", lines[1])
	}
}

func TestParseHMRCSkipsBrokenRows(t *testing.T) {
	raw := "[bad,row]<0,1>x\n[1000,500]<0,100>好\n[]<0,1>y\n"
	lines := parseHMRC(raw)
	if len(lines) != 1 || lines[0].Text != "好" {
		t.Fatalf("lines = %+v", lines)
	}
}

func TestMergeTranslationsMatchesByTime(t *testing.T) {
	words := []lyrics.Line{{Time: 0, Text: "晴天 - 周杰伦 Jay Chou)"}, {Time: 2.582, Text: "第二行"}}
	plain := []lyrics.Line{{Time: 0.1, Text: "晴天 - 周杰伦 (Jay Chou)", Translation: "Sunny Day"}, {Time: 2.6, Text: "第二行"}}
	got := mergeTranslations(words, plain)
	if got[0].Translation != "Sunny Day" {
		t.Fatalf("translation = %q", got[0].Translation)
	}
	if got[1].Translation != "" {
		t.Fatalf("unexpected translation on second line: %q", got[1].Translation)
	}
	if got[0].Text != "晴天 - 周杰伦 (Jay Chou)" || got[1].Text != "第二行" {
		t.Fatalf("text = %q / %q", got[0].Text, got[1].Text)
	}
}
