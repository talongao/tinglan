package music

import (
	"context"
	"math"
	"sort"
	"strings"
	"unicode"
)

// Matching is two-stage by design:
//
//  1. A cheap, deterministic code pass narrows thousands of rows down to a few
//     similar candidates (normalized title + artist index, duration/album as
//     tie-breakers). Dissimilar pairs are never judged at all.
//  2. Only the gray band is optionally handed to an external judge (Jev). The
//     judge can never merge a pair the code pass considered dissimilar.
type Band string

const (
	BandExact     Band = "exact"
	BandSimilar   Band = "similar"
	BandGray      Band = "gray"
	BandDifferent Band = "different"
)

// Judge is the optional external same-recording oracle. Implementations must be
// safe to call concurrently and must not receive credentials or audio.
type Judge interface {
	SameRecording(ctx context.Context, a, b Track) (float64, error)
}

// MatchPolicy holds the thresholds. Zero values fall back to DefaultMatchPolicy.
type MatchPolicy struct {
	// CandidateFloor is the minimum code similarity for a pair to be considered
	// at all. Pairs below it are reported as different without any judgment.
	CandidateFloor float64
	// AutoMerge is the code similarity at which a pair is merged without asking
	// the judge.
	AutoMerge float64
	// JevMerge is the probability at which a gray-zone pair is merged.
	JevMerge float64
}

func DefaultMatchPolicy() MatchPolicy {
	return MatchPolicy{CandidateFloor: .55, AutoMerge: .90, JevMerge: .80}
}

func (p MatchPolicy) sanitized() MatchPolicy {
	d := DefaultMatchPolicy()
	// Written as positive tests so NaN also falls back to the defaults.
	if !(p.CandidateFloor > 0 && p.CandidateFloor < 1) {
		p.CandidateFloor = d.CandidateFloor
	}
	if !(p.AutoMerge > p.CandidateFloor && p.AutoMerge <= 1) {
		p.AutoMerge = d.AutoMerge
	}
	if !(p.JevMerge > 0 && p.JevMerge <= 1) {
		p.JevMerge = d.JevMerge
	}
	return p
}

// Verdict is the outcome for one candidate pair. Reason is a short, stable
// machine-readable tag used by tests and diagnostics.
type Verdict struct {
	Same   bool
	Band   Band
	Method string // "id", "code" or "jev"
	Score  float64
	Reason string
}

// Candidate is one similar row found by the code pass.
type Candidate struct {
	Track Track
	Score float64
	Band  Band
}

// Fold normalizes text for comparison: full-width to half-width, lower case,
// punctuation and whitespace removed. It never removes CJK characters.
func Fold(text string) string {
	var b strings.Builder
	b.Grow(len(text))
	for _, r := range text {
		switch {
		case r >= '！' && r <= '～': // full-width ASCII block
			r = r - 0xFEE0
		case r == '　':
			r = ' '
		}
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

var versionMarkers = []struct {
	token string
	set   func(*Version)
}{
	{"live", func(v *Version) { v.Live = true }},
	{"现场", func(v *Version) { v.Live = true }},
	{"演唱会", func(v *Version) { v.Live = true }},
	{"remix", func(v *Version) { v.Remix = true }},
	{"混音", func(v *Version) { v.Remix = true }},
	{"acoustic", func(v *Version) { v.Acoustic = true }},
	{"unplugged", func(v *Version) { v.Acoustic = true }},
	{"不插电", func(v *Version) { v.Acoustic = true }},
	{"instrumental", func(v *Version) { v.Instrumental = true }},
	{"offvocal", func(v *Version) { v.Instrumental = true }},
	{"伴奏", func(v *Version) { v.Instrumental = true }},
	{"纯音乐", func(v *Version) { v.Instrumental = true }},
	{"cover", func(v *Version) { v.Cover = true }},
	{"翻唱", func(v *Version) { v.Cover = true }},
	{"demo", func(v *Version) { v.Demo = true }},
	{"小样", func(v *Version) { v.Demo = true }},
	{"合唱版", func(v *Version) { v.Edition = "chorus" }},
	{"合唱", func(v *Version) { v.Edition = "chorus" }},
	{"重生版", func(v *Version) { v.Edition = "reborn" }},
	{"重制版", func(v *Version) { v.Edition = "remaster" }},
	{"重置版", func(v *Version) { v.Edition = "remaster" }},
	{"加速版", func(v *Version) { v.Edition = "speedup" }},
	{"慢速版", func(v *Version) { v.Edition = "slow" }},
	{"dj版", func(v *Version) { v.Remix = true }},
}

// DetectVersion extracts performance markers from free-form provider metadata.
// Adapters that receive explicit flags should set them instead.
func DetectVersion(parts ...string) Version {
	var v Version
	for _, part := range parts {
		lower := strings.ToLower(part)
		for _, marker := range versionMarkers {
			if strings.Contains(lower, marker.token) {
				marker.set(&v)
			}
		}
	}
	return v
}

// CleanTitle removes version markers and bracketed annotations so that
// "晴天 (Live)" and "晴天" share a candidate bucket while keeping their
// Version values distinct.
func CleanTitle(title string) string {
	out := strings.Builder{}
	depth := 0
	for _, r := range title {
		switch r {
		case '(', '（', '[', '【':
			depth++
			continue
		case ')', '）', ']', '】':
			if depth > 0 {
				depth--
			}
			continue
		}
		if depth == 0 {
			out.WriteRune(r)
		}
	}
	cleaned := out.String()
	lower := strings.ToLower(cleaned)
	for _, marker := range versionMarkers {
		lower = strings.ReplaceAll(lower, marker.token, " ")
	}
	return strings.Join(strings.Fields(lower), " ")
}

// TitleKey is the bucket key for the candidate index.
func TitleKey(title string) string { return Fold(CleanTitle(title)) }

// splitArtists flattens provider artist strings that already contain several
// names ("A / B", "A feat. B").
func splitArtists(name string) []string {
	// Feature credits appear mid-string and would otherwise fold into the first
	// artist ("jay chou feat. X" -> "jaychoufeatx").
	normalized := name
	for _, marker := range []string{" feat. ", " feat ", " ft. ", " ft "} {
		normalized = replaceFold(normalized, marker, " / ")
	}
	fields := strings.FieldsFunc(normalized, func(r rune) bool {
		switch r {
		case '/', '&', ',', '，', '、', ';', '；', '+', '|':
			return true
		}
		return false
	})
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		lower := strings.ToLower(field)
		for _, prefix := range []string{"feat.", "feat ", "ft.", "ft ", "with "} {
			if strings.HasPrefix(lower, prefix) {
				field = strings.TrimSpace(field[len(prefix):])
				break
			}
		}
		if field != "" {
			out = append(out, field)
		}
	}
	return out
}

// replaceFold replaces every case-insensitive occurrence of old with repl.
func replaceFold(text, old, repl string) string {
	lower := strings.ToLower(text)
	var b strings.Builder
	b.Grow(len(text))
	for {
		index := strings.Index(lower, old)
		if index < 0 {
			b.WriteString(text)
			return b.String()
		}
		b.WriteString(text[:index])
		b.WriteString(repl)
		text = text[index+len(old):]
		lower = lower[index+len(old):]
	}
}

// ArtistSet returns folded, deduplicated artist names in sorted order.
func ArtistSet(artists []Artist) []string {
	seen := make(map[string]bool, len(artists))
	out := make([]string, 0, len(artists))
	for _, artist := range artists {
		for _, name := range splitArtists(artist.Name) {
			folded := Fold(name)
			if folded == "" || seen[folded] {
				continue
			}
			seen[folded] = true
			out = append(out, folded)
		}
	}
	sort.Strings(out)
	return out
}

// artistKey is the coarse index key: the first (alphabetically) artist.
func artistKey(artists []Artist) string {
	set := ArtistSet(artists)
	if len(set) == 0 {
		return ""
	}
	return set[0]
}

func artistOverlap(a, b []Artist) float64 {
	left, right := ArtistSet(a), ArtistSet(b)
	if len(left) == 0 && len(right) == 0 {
		return .5
	}
	if len(left) == 0 || len(right) == 0 {
		return .35
	}
	seen := make(map[string]bool, len(left))
	for _, name := range left {
		seen[name] = true
	}
	hits := 0
	for _, name := range right {
		if seen[name] {
			hits++
		}
	}
	// Containment rather than Jaccard: a duet credited to one artist on one
	// source and two on another should still score high.
	return float64(hits) / float64(min(len(left), len(right)))
}

func bigrams(text string) map[string]bool {
	runes := []rune(text)
	out := make(map[string]bool, len(runes))
	if len(runes) == 1 {
		out[string(runes)] = true
		return out
	}
	for i := 0; i+1 < len(runes); i++ {
		out[string(runes[i:i+2])] = true
	}
	return out
}

// titleSimilarity is the Dice coefficient over rune bigrams. It is symmetric,
// cheap and language-agnostic (works for CJK and Latin titles).
func titleSimilarity(a, b string) float64 {
	if a == "" || b == "" {
		return 0
	}
	if a == b {
		return 1
	}
	left, right := bigrams(a), bigrams(b)
	if len(left) == 0 || len(right) == 0 {
		return 0
	}
	hits := 0
	for gram := range left {
		if right[gram] {
			hits++
		}
	}
	return 2 * float64(hits) / float64(len(left)+len(right))
}

func durationSimilarity(a, b *float64) float64 {
	if a == nil || b == nil || *a <= 0 || *b <= 0 {
		return .5
	}
	delta := math.Abs(*a - *b)
	switch {
	case delta <= 2:
		return 1
	case delta >= 15:
		return 0
	default:
		return 1 - (delta-2)/13
	}
}

func albumSimilarity(a, b *Album) float64 {
	if a == nil || b == nil || a.Title == "" || b.Title == "" {
		return .5
	}
	if Fold(a.Title) == Fold(b.Title) {
		return 1
	}
	return titleSimilarity(Fold(a.Title), Fold(b.Title)) * .5
}

// Similarity is the code-only score in [0,1]. Incompatible versions score 0 so
// a live take can never be merged into the studio recording.
func Similarity(a, b Track) float64 {
	if !a.Version.Compatible(b.Version) {
		return 0
	}
	title := titleSimilarity(TitleKey(a.Title), TitleKey(b.Title))
	if title == 0 {
		return 0
	}
	return .50*title + .30*artistOverlap(a.Artists, b.Artists) + .12*durationSimilarity(a.Duration, b.Duration) + .08*albumSimilarity(a.Album, b.Album)
}

// Decide applies the code policy to one pair. It never calls the judge.
func Decide(a, b Track, policy MatchPolicy) Verdict {
	policy = policy.sanitized()
	if a.Source == b.Source && a.ID == b.ID {
		return Verdict{Same: true, Band: BandExact, Method: "id", Score: 1, Reason: "same-source-id"}
	}
	// Editions are a hard gate: a chorus cut or a re-recorded version is a
	// different recording even when title, artists and duration line up.
	if !a.Version.Compatible(b.Version) {
		return Verdict{Same: false, Band: BandDifferent, Method: "version", Score: 0, Reason: "version-mismatch"}
	}
	score := Similarity(a, b)
	switch {
	case score >= policy.AutoMerge:
		return Verdict{Same: true, Band: BandSimilar, Method: "code", Score: score, Reason: "code-auto"}
	case score >= policy.CandidateFloor:
		return Verdict{Same: false, Band: BandGray, Method: "code", Score: score, Reason: "gray-zone"}
	default:
		return Verdict{Same: false, Band: BandDifferent, Method: "code", Score: score, Reason: "below-candidate-floor"}
	}
}

// JudgePair optionally escalates a gray-zone pair to the external judge. A
// judge error keeps the rows separate: uncertainty never merges data.
func JudgePair(ctx context.Context, judge Judge, policy MatchPolicy, a, b Track) Verdict {
	verdict := Decide(a, b, policy)
	if verdict.Band != BandGray || judge == nil {
		return verdict
	}
	probability, err := judge.SameRecording(ctx, a, b)
	if err != nil {
		verdict.Reason = "judge-error"
		return verdict
	}
	policy = policy.sanitized()
	verdict.Score = probability
	verdict.Method = "jev"
	if probability >= policy.JevMerge {
		verdict.Same = true
		verdict.Reason = "judge-merge"
	} else {
		verdict.Reason = "judge-keep-separate"
	}
	return verdict
}

// Index is the candidate index. Buckets are keyed by folded title, which keeps
// candidate lookup near O(1) instead of comparing every pair (5k tracks would
// otherwise be ~12.5M comparisons).
type Index struct {
	byTitle  map[string][]int
	byArtist map[string][]int
}

func NewIndex() *Index {
	return &Index{byTitle: make(map[string][]int), byArtist: make(map[string][]int)}
}

// Add registers a row. Callers keep the returned position stable.
func (i *Index) Add(position int, track Track) {
	title := TitleKey(track.Title)
	i.byTitle[title] = append(i.byTitle[title], position)
	if artist := artistKey(track.Artists); artist != "" {
		i.byArtist[artist] = append(i.byArtist[artist], position)
	}
}

// Candidates returns positions that pass the candidate floor, best first. It
// only looks inside the title bucket and, when the title bucket is empty, the
// primary-artist bucket.
func (i *Index) Candidates(track Track, policy MatchPolicy, score func(int) float64) []int {
	policy = policy.sanitized()
	seen := make(map[int]bool)
	var positions []int
	add := func(list []int) {
		for _, position := range list {
			if seen[position] {
				continue
			}
			seen[position] = true
			if score(position) >= policy.CandidateFloor {
				positions = append(positions, position)
			}
		}
	}
	add(i.byTitle[TitleKey(track.Title)])
	if len(positions) == 0 {
		if artist := artistKey(track.Artists); artist != "" {
			add(i.byArtist[artist])
		}
	}
	sort.SliceStable(positions, func(a, b int) bool { return score(positions[a]) > score(positions[b]) })
	return positions
}
