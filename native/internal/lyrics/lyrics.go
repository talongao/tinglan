package lyrics

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Word struct {
	Time float64 `json:"time"`
	Text string  `json:"text"`
}
type Line struct {
	Time         float64 `json:"time"`
	Text         string  `json:"text"`
	Translation  string  `json:"translation,omitempty"`
	Romanization string  `json:"romanization,omitempty"`
	Words        []Word  `json:"words,omitempty"`
}

var stamp = regexp.MustCompile(`\[(\d+):(\d{2})(?:\.(\d{1,3}))?\]`)
var wordStamp = regexp.MustCompile(`<(\d+):(\d{2})(?:\.(\d{1,3}))?>`)
var offsetStamp = regexp.MustCompile(`(?i)\[offset:([+-]?\d+)\]`)
var metadata = regexp.MustCompile(`^\[[a-zA-Z]+:`)

func seconds(parts []string) float64 {
	minutes, _ := strconv.Atoi(parts[1])
	sec, _ := strconv.Atoi(parts[2])
	fraction := 0.0
	if parts[3] != "" {
		fraction, _ = strconv.ParseFloat("0."+parts[3], 64)
	}
	return float64(minutes*60+sec) + fraction
}
func Parse(raw string) []Line {
	if len(raw) > 512*1024 {
		return nil
	}
	offset := 0.0
	if match := offsetStamp.FindStringSubmatch(raw); match != nil {
		ms, _ := strconv.ParseFloat(match[1], 64)
		offset = ms / 1000
	}
	var lines []Line
	for _, row := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		row = strings.TrimSpace(row)
		matches := stamp.FindAllStringSubmatch(row, -1)
		text := strings.TrimSpace(stamp.ReplaceAllString(row, ""))
		text, romanization, translation := splitInlineSubtitles(text)
		if len(matches) == 0 {
			if text != "" && !metadata.MatchString(text) {
				lines = append(lines, Line{Time: -1, Text: text, Translation: translation, Romanization: romanization})
			}
			continue
		}
		var words []Word
		indices := wordStamp.FindAllStringSubmatchIndex(text, -1)
		for i, idx := range indices {
			end := len(text)
			if i+1 < len(indices) {
				end = indices[i+1][0]
			}
			parts := wordStamp.FindStringSubmatch(text[idx[0]:idx[1]])
			words = append(words, Word{Time: max(0, seconds(parts)+offset), Text: text[idx[1]:end]})
		}
		text = wordStamp.ReplaceAllString(text, "")
		for _, match := range matches {
			lines = append(lines, Line{Time: max(0, seconds(match)+offset), Text: text, Translation: translation, Romanization: romanization, Words: words})
		}
	}
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].Time < lines[j].Time })
	merged := make([]Line, 0, len(lines))
	for _, line := range lines {
		if len(merged) > 0 && line.Time >= 0 && merged[len(merged)-1].Time == line.Time {
			prior := &merged[len(merged)-1]
			// Providers put the credits block on the same timestamp as the
			// first lyric line. Only a short single line can be a subtitle;
			// otherwise the credits would render as a wall of "translation".
			if plausibleSubtitle(line.Text) {
				prior.Translation = appendSubtitle(prior.Translation, line.Text)
			}
			if plausibleSubtitle(line.Translation) {
				prior.Translation = appendSubtitle(prior.Translation, line.Translation)
			}
			if plausibleSubtitle(line.Romanization) {
				prior.Romanization = appendSubtitle(prior.Romanization, line.Romanization)
			}
		} else {
			merged = append(merged, line)
		}
	}
	return merged
}

// SplitInlineSubtitles separates a caret-joined row into original text,
// pronunciation and translation. It is exported because providers that ship
// their own document format (HMRC) use the same convention.
func SplitInlineSubtitles(text string) (string, string, string) {
	return splitInlineSubtitles(text)
}

// Providers also join original, pronunciation and translation with carets.
// Require linguistic content and a CJK segment to preserve literal x^2, etc.
func splitInlineSubtitles(text string) (string, string, string) {
	parts := strings.Split(text, "^")
	if len(parts) < 2 {
		return text, "", ""
	}
	cjk := func(r rune) bool {
		return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul)
	}
	plain := make([]string, len(parts))
	hasCJK := false
	for i, part := range parts {
		plain[i] = strings.TrimSpace(wordStamp.ReplaceAllString(part, ""))
		if strings.IndexFunc(plain[i], unicode.IsLetter) < 0 {
			return text, "", ""
		}
		hasCJK = hasCJK || strings.IndexFunc(plain[i], cjk) >= 0
	}
	if !hasCJK {
		return text, "", ""
	}
	romanization, translation := "", ""
	for _, part := range plain[1:] {
		if strings.IndexFunc(plain[0], cjk) >= 0 && strings.IndexFunc(part, cjk) < 0 && strings.IndexFunc(part, func(r rune) bool { return unicode.In(r, unicode.Latin) }) >= 0 {
			romanization = appendSubtitle(romanization, part)
		} else {
			translation = appendSubtitle(translation, part)
		}
	}
	return strings.TrimSpace(parts[0]), romanization, translation
}

func contains(rows []string, row string) bool {
	for _, existing := range rows {
		if existing == row {
			return true
		}
	}
	return false
}

func appendSubtitle(existing, extra string) string {
	for _, row := range strings.Split(extra, "\n") {
		row = strings.TrimSpace(row)
		if row == "" {
			continue
		}
		found := false
		for _, prior := range strings.Split(existing, "\n") {
			if prior == row {
				found = true
				break
			}
		}
		if !found {
			if existing != "" {
				existing += "\n"
			}
			existing += row
		}
	}
	return existing
}

// Subtitles keeps pronunciation and translation separate but in reading order.
// Providers sometimes ship the same text as both, so repeats are dropped.
func (line Line) Subtitles() []string {
	var rows []string
	for _, value := range []string{line.Romanization, line.Translation} {
		for _, row := range strings.Split(value, "\n") {
			if row = strings.TrimSpace(row); row != "" && !contains(rows, row) {
				rows = append(rows, row)
			}
		}
	}
	return rows
}
func Active(lines []Line, seconds float64) int {
	index := -1
	for i, line := range lines {
		if line.Time < 0 {
			continue
		}
		if line.Time > seconds {
			break
		}
		index = i
	}
	return index
}

// plausibleSubtitle reports whether text looks like one line of translation or
// pronunciation rather than a credits block providers park on line one.
func plausibleSubtitle(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	if strings.ContainsAny(text, "\n\r") {
		return false
	}
	if utf8.RuneCountInString(text) > 80 {
		return false
	}
	return !metadata.MatchString(text)
}
