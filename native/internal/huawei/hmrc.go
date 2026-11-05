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
		// Cantonese rows append the romanisation after a caret, outside the
		// word markers: "<0,300>偏<300,80>偏 ^pin pin". Keeping it inside the
		// text would break both the display line and word-to-glyph alignment.
		romanization := ""
		if caret := strings.IndexByte(rest, '^'); caret >= 0 {
			romanization = strings.TrimSpace(rest[caret+1:])
			rest = rest[:caret]
		}
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
		item := lyrics.Line{Time: start / 1000, Text: body, Romanization: romanization, Words: words}
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

// mergeTranslations fills subtitles from the plain lyric track. The shared
// helper in internal/lyrics is the single implementation of the rule.
func mergeTranslations(lines, plain []lyrics.Line) []lyrics.Line {
	return lyrics.MergeSubtitles(lines, plain)
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
