package lyrics

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// TTML parsing limits. Anything larger is rejected instead of truncated so a
// partial timeline is never mistaken for a complete one.
const (
	maxTTMLBytes = 512 * 1024
	maxTTMLDepth = 32
	maxTTMLLines = 5000
	maxTTMLWords = 100000
)

// ErrTTMLTooLarge reports a document beyond the byte, depth, line or word cap.
var ErrTTMLTooLarge = errors.New("lyrics: TTML document exceeds limits")

// ErrTTMLSeqTiming reports timeContainer="seq", which this parser does not
// support yet. Refusing beats emitting a wrong timeline.
var ErrTTMLSeqTiming = errors.New("lyrics: TTML timeContainer=seq is not supported")

// ErrTTMLStructure reports malformed XML or a document with no usable cues.
var ErrTTMLStructure = errors.New("lyrics: malformed TTML")

// ParseAny sniffs the document format and parses it. The returned label is
// "ttml", "lrc" or "" when the content was empty. TTML failures fall back to
// the LRC parser so a mislabeled file still shows plain lines when possible.
func ParseAny(raw string) ([]Line, string) {
	if strings.TrimSpace(raw) == "" {
		return nil, ""
	}
	if looksLikeTTML(raw) {
		if lines, err := ParseTTML(raw); err == nil && len(lines) > 0 {
			return lines, "ttml"
		}
	}
	lines := Parse(raw)
	if len(lines) == 0 {
		return nil, ""
	}
	return lines, "lrc"
}

func looksLikeTTML(raw string) bool {
	head := raw
	if len(head) > 4096 {
		head = head[:4096]
	}
	lower := strings.ToLower(head)
	return strings.Contains(lower, "<tt") && (strings.Contains(lower, "http://www.w3.org/ns/ttml") ||
		strings.Contains(lower, "<body") || strings.Contains(lower, "<p "))
}

// ParseTTML parses an Apple-style TTML document into timed lines with optional
// per-word timings. Times are resolved against their enclosing container: a
// span inside <p begin="10s" dur="4s"> with begin="1s" starts at 11s and is
// clipped to the paragraph end.
func ParseTTML(raw string) ([]Line, error) {
	if len(raw) > maxTTMLBytes {
		return nil, ErrTTMLTooLarge
	}
	decoder := xml.NewDecoder(strings.NewReader(raw))
	decoder.Strict = false
	decoder.Entity = xml.HTMLEntity

	var (
		lines   []Line
		stack   []ttmlContainer
		depth   int
		words   int
		pending *ttmlLine
		sawRoot bool
	)
	flush := func() error {
		if pending == nil {
			return nil
		}
		line := pending.finish()
		pending = nil
		if line == nil {
			return nil
		}
		lines = append(lines, *line)
		if len(lines) > maxTTMLLines {
			return ErrTTMLTooLarge
		}
		return nil
	}
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrTTMLStructure, err)
		}
		switch node := token.(type) {
		case xml.StartElement:
			depth++
			if depth > maxTTMLDepth {
				return nil, ErrTTMLTooLarge
			}
			name := strings.ToLower(node.Name.Local)
			if ttmlSeqTiming(node.Attr) {
				return nil, ErrTTMLSeqTiming
			}
			switch name {
			case "tt":
				sawRoot = true
			case "p":
				if err := flush(); err != nil {
					return nil, err
				}
				parentBegin, parentEnd := currentSpan(stack)
				parent := parentEnd
				begin, dur, err := ttmlTiming(node.Attr, parentBegin, parentEnd)
				if err != nil {
					return nil, err
				}
				line := &ttmlLine{begin: begin, dur: dur, parentEnd: parent}
				pending = line
				stack = append(stack, ttmlContainer{begin: begin, end: endOf(begin, dur, parent)})
			case "span":
				parentBegin, parentEnd := currentSpan(stack)
				parent := parentEnd
				begin, dur, err := ttmlTiming(node.Attr, parentBegin, parentEnd)
				if err != nil {
					return nil, err
				}
				role := ttmlRole(node.Attr)
				if pending != nil && role == "" {
					text := ""
					if err := decoder.DecodeElement(&text, &node); err != nil {
						return nil, fmt.Errorf("%w: %v", ErrTTMLStructure, err)
					}
					depth--
					words++
					if words > maxTTMLWords {
						return nil, ErrTTMLTooLarge
					}
					text = strings.TrimSpace(text)
					if text != "" {
						start, end := clampSpan(begin, endOf(begin, dur, parent), parent)
						pending.addWord(ttmlWord{start: start, end: end, text: text})
					}
					continue
				}
				stack = append(stack, ttmlContainer{begin: begin, end: endOf(begin, dur, parent), role: role})
			case "br":
				if pending != nil {
					pending.addBreak()
				}
			}
		case xml.EndElement:
			if depth > 0 {
				depth--
			}
			name := strings.ToLower(node.Name.Local)
			if name == "p" {
				if err := flush(); err != nil {
					return nil, err
				}
			}
			if (name == "p" || name == "span") && len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			if pending == nil {
				continue
			}
			role := currentRole(stack)
			if role != "" {
				pending.addRoleText(role, string(node))
				continue
			}
			pending.addText(string(node))
		}
	}
	if err := flush(); err != nil {
		return nil, err
	}
	if !sawRoot {
		return nil, ErrTTMLStructure
	}
	lines = finishTTMLLines(lines)
	if len(lines) == 0 {
		return nil, ErrTTMLStructure
	}
	return lines, nil
}

type ttmlContainer struct {
	begin float64
	end   float64
	role  string
}

type ttmlWord struct {
	start, end float64
	text       string
}

type ttmlLine struct {
	begin     float64
	dur       float64
	parentEnd float64
	body      strings.Builder
	words     []ttmlWord
	wordBuf   strings.Builder
	trans     []string
	roman     []string
	hasTime   bool
}

func (l *ttmlLine) addText(text string) {
	if strings.TrimSpace(text) == "" {
		// Indentation between spans must not leak into the sung text; a single
		// space inside a line still separates Latin words.
		if strings.ContainsAny(text, "\n\r") || l.body.Len() == 0 {
			return
		}
		text = " "
	}
	l.body.WriteString(text)
	if len(l.words) > 0 || l.wordBuf.Len() > 0 {
		l.wordBuf.WriteString(text)
	}
}

func (l *ttmlLine) addBreak() {
	l.body.WriteString("\n")
	if l.wordBuf.Len() > 0 {
		l.wordBuf.WriteString(" ")
	}
}

func (l *ttmlLine) addWord(word ttmlWord) {
	if l.wordBuf.Len() > 0 {
		l.wordBuf.Reset()
	}
	l.words = append(l.words, word)
	l.body.WriteString(word.text)
	l.hasTime = l.hasTime || word.end > word.start
}

func (l *ttmlLine) addRoleText(role, text string) {
	switch role {
	case "translation":
		l.trans = append(l.trans, text)
	case "transcription", "romanization":
		l.roman = append(l.roman, text)
	}
}

func (l *ttmlLine) finish() *Line {
	text := strings.TrimSpace(l.body.String())
	text = collapseSpaces(text)
	if text == "" && len(l.trans) == 0 && len(l.roman) == 0 {
		return nil
	}
	line := &Line{
		Time:         max(0, l.begin),
		Text:         text,
		Translation:  strings.TrimSpace(strings.Join(l.trans, "\n")),
		Romanization: strings.TrimSpace(strings.Join(l.roman, "\n")),
	}
	if len(l.words) > 0 {
		words := make([]Word, 0, len(l.words))
		for _, word := range l.words {
			start := max(0, word.start)
			if l.parentEnd > 0 {
				start = min(start, l.parentEnd)
			}
			words = append(words, Word{Time: start, Text: word.text})
		}
		line.Words = words
	}
	return line
}

func finishTTMLLines(lines []Line) []Line {
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].Time < lines[j].Time })
	merged := make([]Line, 0, len(lines))
	for _, line := range lines {
		if len(merged) > 0 && merged[len(merged)-1].Time == line.Time {
			prior := &merged[len(merged)-1]
			if prior.Text == "" {
				prior.Text = line.Text
			} else if line.Text != "" && line.Text != prior.Text {
				prior.Translation = appendSubtitle(prior.Translation, line.Text)
			}
			prior.Translation = appendSubtitle(prior.Translation, line.Translation)
			prior.Romanization = appendSubtitle(prior.Romanization, line.Romanization)
			continue
		}
		merged = append(merged, line)
	}
	return merged
}

func currentSpan(stack []ttmlContainer) (begin, end float64) {
	for i := len(stack) - 1; i >= 0; i-- {
		if stack[i].end > 0 {
			return stack[i].begin, stack[i].end
		}
	}
	return 0, 0
}

func currentRole(stack []ttmlContainer) string {
	for i := len(stack) - 1; i >= 0; i-- {
		if stack[i].role != "" {
			return stack[i].role
		}
	}
	return ""
}

// clampSpan keeps word times inside the enclosing time container so malformed
// documents cannot produce a word that starts after it ends.
func clampSpan(begin, end, parentEnd float64) (float64, float64) {
	if parentEnd <= 0 {
		return begin, end
	}
	if begin > parentEnd {
		begin = parentEnd
	}
	if end < begin {
		end = begin
	}
	if end > parentEnd {
		end = parentEnd
	}
	return begin, end
}

func endOf(begin, dur, parentEnd float64) float64 {
	end := begin + dur
	if dur <= 0 {
		end = 0
	}
	if parentEnd > 0 && (end == 0 || end > parentEnd) {
		end = parentEnd
	}
	return end
}

// ttmlTiming reads begin/end/dur from attributes. begin is relative to the
// enclosing container when it is not an absolute clock value.
func ttmlTiming(attrs []xml.Attr, parentBegin, parentEnd float64) (begin, dur float64, err error) {
	var beginRaw, endRaw, durRaw string
	for _, attr := range attrs {
		switch strings.ToLower(attr.Name.Local) {
		case "begin":
			beginRaw = strings.TrimSpace(attr.Value)
		case "end":
			endRaw = strings.TrimSpace(attr.Value)
		case "dur":
			durRaw = strings.TrimSpace(attr.Value)
		}
	}
	if beginRaw == "" {
		return 0, 0, nil
	}
	begin, err = parseTTMLTime(beginRaw)
	if err != nil {
		return 0, 0, err
	}
	// Apple Music writes absolute media times, while TTML par containers allow
	// child times relative to the parent. A child that starts before its parent
	// can only be relative, so rebase those and leave absolute times untouched.
	if parentBegin > 0 && begin < parentBegin {
		begin += parentBegin
	}
	if durRaw != "" {
		dur, err = parseTTMLTime(durRaw)
		if err != nil {
			return 0, 0, err
		}
		return begin, dur, nil
	}
	if endRaw != "" {
		end, err := parseTTMLTime(endRaw)
		if err != nil {
			return 0, 0, err
		}
		if parentBegin > 0 && end < parentBegin {
			end += parentBegin
		}
		if end > begin {
			dur = end - begin
		}
	}
	return begin, dur, nil
}

func ttmlSeqTiming(attrs []xml.Attr) bool {
	for _, attr := range attrs {
		if strings.EqualFold(attr.Name.Local, "timecontainer") &&
			strings.EqualFold(strings.TrimSpace(attr.Value), "seq") {
			return true
		}
	}
	return false
}

func ttmlRole(attrs []xml.Attr) string {
	for _, attr := range attrs {
		if strings.EqualFold(attr.Name.Local, "role") {
			value := strings.ToLower(strings.TrimSpace(attr.Value))
			if idx := strings.Index(value, ":"); idx >= 0 {
				value = value[idx+1:]
			}
			// Apple writes x-translation / x-transcription; accept both.
			value = strings.TrimPrefix(value, "x-")
			switch value {
			case "translation", "transcription", "romanization":
				return value
			}
		}
	}
	return ""
}

// parseTTMLTime accepts clock values (hh:mm:ss.mmm, mm:ss.mmm), offset times
// (12.5s, 500ms, 3f, 10t) and bare seconds.
func parseTTMLTime(raw string) (float64, error) {
	value := strings.TrimSpace(strings.ToLower(raw))
	if value == "" {
		return 0, nil
	}
	if strings.Contains(value, ":") {
		parts := strings.Split(value, ":")
		total := 0.0
		for _, part := range parts {
			number, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
			if err != nil {
				return 0, fmt.Errorf("%w: bad clock %q", ErrTTMLStructure, raw)
			}
			total = total*60 + number
		}
		return total, nil
	}
	unit := ""
	number := value
	for _, suffix := range []string{"ms", "h", "m", "s", "f", "t"} {
		if strings.HasSuffix(value, suffix) {
			unit = suffix
			number = strings.TrimSuffix(value, suffix)
			break
		}
	}
	amount, err := strconv.ParseFloat(strings.TrimSpace(number), 64)
	if err != nil {
		return 0, fmt.Errorf("%w: bad time %q", ErrTTMLStructure, raw)
	}
	switch unit {
	case "h":
		return amount * 3600, nil
	case "m":
		return amount * 60, nil
	case "ms":
		return amount / 1000, nil
	case "f":
		return amount / 30, nil
	case "t":
		return amount / 1e7, nil
	}
	return amount, nil
}

func collapseSpaces(text string) string {
	fields := strings.FieldsFunc(text, func(r rune) bool { return r == '\n' })
	for i, field := range fields {
		fields[i] = strings.Join(strings.Fields(field), " ")
	}
	out := strings.Join(fields, "\n")
	return strings.TrimSpace(out)
}
