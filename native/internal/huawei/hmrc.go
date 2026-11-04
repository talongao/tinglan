package huawei

import (
	"strconv"
	"strings"
	"unicode"

	"tinglan/native/internal/lyrics"
)

// parseHMRC turns the provider's word-by-word lyric document into the neutral
// line form. The format is one line per row:
//
//	[lineStart,lineDuration]<wordOffset,wordDuration>text<offset,duration>text
//
// All values are milliseconds; word offsets are relative to the line start and
// the line start is relative to the beginning of the audio.
func parseHMRC(raw string) []lyrics.Line {
	var out []lyrics.Line
	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "[ti:") || strings.HasPrefix(line, "[ar:") ||
			strings.HasPrefix(line, "[al:") || strings.HasPrefix(line, "[by:") || strings.HasPrefix(line, "[offset:") {
			continue
		}
		if !strings.HasPrefix(line, "[") {
			continue
		}
		end := strings.IndexByte(line, ']')
		if end < 0 {
			continue
		}
		start, duration, ok := pair(line[1:end])
		if !ok {
			continue
		}
		rest := line[end+1:]
		var words []lyrics.Word
		var text strings.Builder
		for {
			open := strings.IndexByte(rest, '<')
			if open < 0 {
				break
			}
			close := strings.IndexByte(rest[open:], '>')
			if close < 0 {
				break
			}
			offset, length, ok := pair(rest[open+1 : open+close])
			if !ok {
				break
			}
			rest = rest[open+close+1:]
			next := strings.IndexByte(rest, '<')
			var chunk string
			if next < 0 {
				chunk, rest = rest, ""
			} else {
				chunk, rest = rest[:next], rest[next:]
			}
			if chunk == "" {
				continue
			}
			_ = length
			words = append(words, lyrics.Word{
				Time: (start + offset) / 1000,
				Text: chunk,
			})
			text.WriteString(chunk)
		}
		body := strings.TrimSpace(text.String())
		if body == "" {
			continue
		}
		item := lyrics.Line{Time: start / 1000, Text: body, Words: words}
		_ = duration
		out = append(out, item)
	}
	return out
}

// pair reads a "a,b" milliseconds pair.
func pair(raw string) (float64, float64, bool) {
	parts := strings.Split(raw, ",")
	if len(parts) != 2 {
		return 0, 0, false
	}
	first, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	if err != nil {
		return 0, 0, false
	}
	second, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err != nil {
		return 0, 0, false
	}
	return first, second, true
}

// mergeTranslations copies translation and romanization text from the plain
// lyric onto the word-by-word lines. Providers ship them as separate tracks,
// so the only reliable link is the line start time. When both lines carry the
// same words, the plain text wins for display: the word-by-word document
// sometimes drops punctuation, and the two only differ there.
func mergeTranslations(lines, plain []lyrics.Line) []lyrics.Line {
	if len(lines) == 0 || len(plain) == 0 {
		return lines
	}
	for i := range lines {
		var best *lyrics.Line
		bestDelta := 0.6
		for j := range plain {
			delta := plain[j].Time - lines[i].Time
			if delta < 0 {
				delta = -delta
			}
			if delta <= bestDelta {
				best, bestDelta = &plain[j], delta
			}
		}
		if best == nil {
			continue
		}
		if sameWords(lines[i].Text, best.Text) {
			lines[i].Text = best.Text
		}
		if best.Translation != "" && lines[i].Translation == "" {
			lines[i].Translation = best.Translation
		}
		if best.Romanization != "" && lines[i].Romanization == "" {
			lines[i].Romanization = best.Romanization
		}
	}
	return lines
}

func normalizeLine(text string) string {
	var out []rune
	for _, r := range text {
		if unicode.IsSpace(r) || unicode.IsPunct(r) {
			continue
		}
		out = append(out, unicode.ToLower(r))
	}
	return string(out)
}

// sameWords compares two lyric lines ignoring spacing and punctuation.
func sameWords(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	left, right := normalizeLine(a), normalizeLine(b)
	return left == right && left != ""
}
