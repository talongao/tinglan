package lyrics

import "unicode"

// MergeSubtitles fills translation and romanization text on word-by-word
// lines from a second lyric track. Providers ship subtitles as separate
// tracks, so the line start time is the only reliable link. When both lines
// carry the same words the plain text wins for display: word-by-word
// documents sometimes drop punctuation and the two differ only there.
func MergeSubtitles(lines, extra []Line) []Line {
	if len(lines) == 0 || len(extra) == 0 {
		return lines
	}
	for i := range lines {
		var best *Line
		bestDelta := 0.6
		for j := range extra {
			delta := extra[j].Time - lines[i].Time
			if delta < 0 {
				delta = -delta
			}
			if delta <= bestDelta {
				best, bestDelta = &extra[j], delta
			}
		}
		if best == nil {
			continue
		}
		if SameWords(lines[i].Text, best.Text) {
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

// HasSubtitles reports whether any line carries a translation or romanization.
func HasSubtitles(lines []Line) bool {
	for _, line := range lines {
		if line.Translation != "" || line.Romanization != "" {
			return true
		}
	}
	return false
}

// SameWords compares two lyric lines ignoring spacing and punctuation.
func SameWords(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return normalize(a) == normalize(b)
}

func normalize(text string) string {
	var out []rune
	for _, r := range text {
		if unicode.IsSpace(r) || unicode.IsPunct(r) {
			continue
		}
		out = append(out, unicode.ToLower(r))
	}
	return string(out)
}
