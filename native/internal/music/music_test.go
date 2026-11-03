package music

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"
)

func ptr(value float64) *float64 { return &value }

func huaweiTrack(id, title string, artists ...string) Track {
	people := make([]Artist, 0, len(artists))
	for _, name := range artists {
		people = append(people, Artist{Name: name})
	}
	return Track{
		Source: SourceHuawei, ID: id, Title: title, Artists: people,
		Album: &Album{Title: "叶惠美"}, Duration: ptr(269), Version: DetectVersion(title),
	}
}

func TestFoldNormalizesWidthSpaceAndPunctuation(t *testing.T) {
	cases := map[string]string{
		"Ｈｅｌｌｏ　Ｗｏｒｌｄ": "helloworld",
		"晴天 (Live)":   "晴天live",
		"Ａ／Ｂ　-　C":     "abc",
	}
	for input, want := range cases {
		if got := Fold(input); got != want {
			t.Fatalf("Fold(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestDetectVersionSeparatesLiveFromStudio(t *testing.T) {
	if !DetectVersion("晴天 (Live)").Live {
		t.Fatal("expected live marker")
	}
	if !DetectVersion("晴天（现场版）").Live {
		t.Fatal("expected live marker for CJK annotation")
	}
	if !DetectVersion("七里香 (Remix)").Remix {
		t.Fatal("expected remix marker")
	}
	if !DetectVersion("卡农 伴奏").Instrumental {
		t.Fatal("expected instrumental marker")
	}
	if !DetectVersion("后来 翻唱").Cover {
		t.Fatal("expected cover marker")
	}
	if got := DetectVersion("七里香"); !got.IsZero() {
		t.Fatalf("plain title should be studio, got %q", got.Key())
	}
}

func TestCleanTitleStripsAnnotationsButKeepsBucket(t *testing.T) {
	if got := CleanTitle("晴天 (Live)"); got != "晴天" {
		t.Fatalf("CleanTitle = %q, want 晴天", got)
	}
	if got := CleanTitle("【纯音乐】卡农"); got != "卡农" {
		t.Fatalf("CleanTitle = %q, want 卡农", got)
	}
	if TitleKey("晴天") != TitleKey("晴天 (Live)") {
		t.Fatal("version annotation must not change the candidate bucket")
	}
}

func TestArtistSetSplitsCollaborations(t *testing.T) {
	got := ArtistSet([]Artist{{Name: "周杰伦 / 费玉清"}, {Name: "Jay Chou feat. 费玉清"}})
	want := []string{"jaychou", "周杰伦", "费玉清"}
	if len(got) != len(want) {
		t.Fatalf("ArtistSet = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ArtistSet = %v, want %v", got, want)
		}
	}
}

func TestSimilarityMergesSameSongAcrossSources(t *testing.T) {
	huawei := huaweiTrack("h1", "晴天", "周杰伦")
	netease := Track{Source: SourceNetEase, ID: "n1", Title: "晴天", Artists: []Artist{{Name: "周杰伦"}}, Duration: ptr(270)}
	score := Similarity(huawei, netease)
	if score < .9 {
		t.Fatalf("similarity = %v, want >= .9", score)
	}
	if verdict := Decide(huawei, netease, DefaultMatchPolicy()); !verdict.Same {
		t.Fatalf("expected merge, got %+v", verdict)
	}
}

func TestSimilarityRejectsDifferentSongsAndVersions(t *testing.T) {
	huawei := huaweiTrack("h1", "晴天", "周杰伦")
	other := Track{Source: SourceNetEase, ID: "n2", Title: "稻香", Artists: []Artist{{Name: "周杰伦"}}, Duration: ptr(223)}
	if verdict := Decide(huawei, other, DefaultMatchPolicy()); verdict.Band != BandDifferent || verdict.Same {
		t.Fatalf("expected different, got %+v", verdict)
	}
	live := Track{Source: SourceNetEase, ID: "n3", Title: "晴天 (Live)", Artists: []Artist{{Name: "周杰伦"}}, Duration: ptr(269), Version: DetectVersion("晴天 (Live)")}
	if verdict := Decide(huawei, live, DefaultMatchPolicy()); verdict.Same {
		t.Fatalf("live take must not merge into studio: %+v", verdict)
	}
	if score := Similarity(huawei, live); score != 0 {
		t.Fatalf("incompatible version similarity = %v, want 0", score)
	}
}

func TestDecideUsesExactSourceIdentity(t *testing.T) {
	track := huaweiTrack("h1", "晴天", "周杰伦")
	verdict := Decide(track, track, DefaultMatchPolicy())
	if !verdict.Same || verdict.Band != BandExact || verdict.Method != "id" {
		t.Fatalf("expected exact identity, got %+v", verdict)
	}
}

type stubJudge struct {
	score float64
	err   error
	calls int
}

func (s *stubJudge) SameRecording(context.Context, Track, Track) (float64, error) {
	s.calls++
	return s.score, s.err
}

func TestJudgePairOnlyRunsInGrayZone(t *testing.T) {
	judge := &stubJudge{score: .95}
	policy := DefaultMatchPolicy()
	exact := huaweiTrack("h1", "晴天", "周杰伦")
	identical := Track{Source: SourceNetEase, ID: "n1", Title: "晴天", Artists: []Artist{{Name: "周杰伦"}}, Duration: ptr(269)}
	if verdict := JudgePair(context.Background(), judge, policy, exact, identical); verdict.Band != BandSimilar {
		t.Fatalf("high-code pair should auto merge without judge: %+v", verdict)
	}
	if judge.calls != 0 {
		t.Fatalf("judge called %d times for a non-gray pair", judge.calls)
	}

	// Partial artist credit drops the pair into the gray band.
	gray := Track{Source: SourceNetEase, ID: "n2", Title: "晴天", Artists: []Artist{{Name: "群星"}}, Duration: ptr(269)}
	before := Decide(exact, gray, policy)
	if before.Band != BandGray {
		t.Fatalf("expected gray band, got %+v", before)
	}
	verdict := JudgePair(context.Background(), judge, policy, exact, gray)
	if judge.calls != 1 {
		t.Fatalf("judge calls = %d, want 1", judge.calls)
	}
	if !verdict.Same || verdict.Method != "jev" {
		t.Fatalf("expected judge merge, got %+v", verdict)
	}
}

func TestJudgePairKeepsRowsSeparateOnError(t *testing.T) {
	judge := &stubJudge{err: errors.New("network down")}
	policy := DefaultMatchPolicy()
	left := huaweiTrack("h1", "晴天", "周杰伦")
	right := Track{Source: SourceNetEase, ID: "n2", Title: "晴天", Artists: []Artist{{Name: "群星"}}, Duration: ptr(269)}
	verdict := JudgePair(context.Background(), judge, policy, left, right)
	if verdict.Same {
		t.Fatalf("judge error must not merge: %+v", verdict)
	}
	if verdict.Reason != "judge-error" {
		t.Fatalf("reason = %q, want judge-error", verdict.Reason)
	}
}

func TestJudgePairRespectsThreshold(t *testing.T) {
	judge := &stubJudge{score: .70}
	policy := DefaultMatchPolicy()
	left := huaweiTrack("h1", "晴天", "周杰伦")
	right := Track{Source: SourceNetEase, ID: "n2", Title: "晴天", Artists: []Artist{{Name: "群星"}}, Duration: ptr(269)}
	verdict := JudgePair(context.Background(), judge, policy, left, right)
	if verdict.Same {
		t.Fatalf("score below JevMerge must keep rows separate: %+v", verdict)
	}
}

func TestMatchPolicySanitizesOutOfRangeValues(t *testing.T) {
	policy := MatchPolicy{CandidateFloor: -1, AutoMerge: 4, JevMerge: math.NaN()}.sanitized()
	want := DefaultMatchPolicy()
	if policy != want {
		t.Fatalf("sanitized = %+v, want %+v", policy, want)
	}
}

func TestIndexFindsOnlySimilarCandidates(t *testing.T) {
	index := NewIndex()
	rows := []Track{
		huaweiTrack("h1", "晴天", "周杰伦"),
		huaweiTrack("h2", "稻香", "周杰伦"),
		huaweiTrack("h3", "七里香", "周杰伦"),
	}
	for position, row := range rows {
		index.Add(position, row)
	}
	probe := Track{Source: SourceNetEase, ID: "n1", Title: "晴天", Artists: []Artist{{Name: "周杰伦"}}, Duration: ptr(268)}
	got := index.Candidates(probe, DefaultMatchPolicy(), func(position int) float64 {
		return Similarity(probe, rows[position])
	})
	if len(got) != 1 || got[0] != 0 {
		t.Fatalf("candidates = %v, want [0]", got)
	}
}

func TestCanonicalTrackKeepsImportOrderAcrossSources(t *testing.T) {
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	huawei := huaweiTrack("h1", "晴天", "周杰伦")
	row := NewCanonical(huawei, 7, now)
	if row.ID != CanonicalID(huawei) {
		t.Fatalf("canonical id = %q", row.ID)
	}

	netease := Track{Source: SourceNetEase, ID: "n1", Title: "晴天", Artists: []Artist{{Name: "周杰伦"}}, CoverURL: "https://cdn.example/cover.jpg"}
	if !row.PutLink(Link(netease, 9, now), now) {
		t.Fatal("expected change when adding a second source")
	}
	links := row.SortedLinks()
	if len(links) != 2 || links[0].Source != SourceHuawei || links[1].Source != SourceNetEase {
		t.Fatalf("links = %+v", links)
	}
	if sources := row.Sources(); len(sources) != 2 || sources[0] != SourceHuawei {
		t.Fatalf("sources = %v", sources)
	}

	// Re-import refreshes metadata but must not reorder the probe sequence.
	refreshed := netease
	refreshed.Title = "晴天 (Remastered)"
	row.PutLink(Link(refreshed, 1, now), now)
	if links := row.SortedLinks(); links[0].Source != SourceHuawei || links[1].ImportSeq != 9 {
		t.Fatalf("re-import must not reset import sequence: %+v", links)
	}
	if link, ok := row.Link(SourceNetEase); !ok || link.Title != "晴天 (Remastered)" {
		t.Fatalf("metadata not refreshed: %+v", link)
	}
	if row.CreatedAt != now || row.UpdatedAt != now {
		t.Fatalf("timestamps = %v / %v", row.CreatedAt, row.UpdatedAt)
	}
}

func TestCanonicalTrackCoversAreDeduped(t *testing.T) {
	now := time.Now()
	row := NewCanonical(huaweiTrack("h1", "晴天", "周杰伦"), 1, now)
	row.CoverURL = "https://cdn.example/a.jpg"
	row.Links[0].CoverURL = "https://cdn.example/a.jpg"
	netease := Track{Source: SourceNetEase, ID: "n1", Title: "晴天", CoverURL: "https://cdn.example/b.jpg"}
	row.PutLink(Link(netease, 2, now), now)
	covers := row.CoverCandidates()
	if len(covers) != 2 || covers[0] != "https://cdn.example/a.jpg" || covers[1] != "https://cdn.example/b.jpg" {
		t.Fatalf("covers = %v", covers)
	}
}

func TestAccessParsingIsThreeState(t *testing.T) {
	if AccessUnknown.String() != "unknown" || AccessFull.String() != "full" || AccessPreview.String() != "preview" {
		t.Fatal("unexpected access labels")
	}
	if ParseAccess("FULL") != AccessFull || ParseAccess("preview") != AccessPreview || ParseAccess("maybe") != AccessUnknown {
		t.Fatal("unexpected access parsing")
	}
}

func TestSourceIDValidation(t *testing.T) {
	if !SourceHuawei.Valid() || !SourceNetEase.Valid() {
		t.Fatal("built-in sources must be valid")
	}
	for _, invalid := range []SourceID{"", "a/b", "a:b", "a b", SourceID(make([]byte, 41))} {
		if invalid.Valid() {
			t.Fatalf("%q should be invalid", invalid)
		}
	}
}

func TestPlaybackIsRedacted(t *testing.T) {
	playback := Playback{URL: "https://secret.example/audio.mp3", LyricURL: "https://secret.example/lrc"}
	if got := playback.String(); got != "[transient playback]" {
		t.Fatalf("String() = %q", got)
	}
	if got := playback.GoString(); got != "[transient playback]" {
		t.Fatalf("GoString() = %q", got)
	}
}

func TestDecideRejectsDifferentEditions(t *testing.T) {
	base := Track{Source: SourceHuawei, ID: "a", Title: "好心分手", Artists: []Artist{{Name: "卢巧音"}}}
	chorus := Track{Source: SourceHuawei, ID: "b", Title: "好心分手(合唱版)", Artists: []Artist{{Name: "卢巧音/王力宏"}}}
	base.Version = DetectVersion(base.Title)
	chorus.Version = DetectVersion(chorus.Title)
	if base.Version.Edition != "" || chorus.Version.Edition != "chorus" {
		t.Fatalf("editions: %q %q", base.Version.Edition, chorus.Version.Edition)
	}
	if v := Decide(base, chorus, DefaultMatchPolicy()); v.Same {
		t.Fatalf("chorus cut merged: %+v", v)
	}
	reborn := Track{Source: SourceHuawei, ID: "c", Title: "我的秘密(G.E.M.重生版)", Artists: []Artist{{Name: "G.E.M.邓紫棋"}}}
	reborn.Version = DetectVersion(reborn.Title)
	if reborn.Version.Edition != "reborn" {
		t.Fatalf("reborn edition: %q", reborn.Version.Edition)
	}
	if v := Decide(base, reborn, DefaultMatchPolicy()); v.Same {
		t.Fatalf("reborn version merged: %+v", v)
	}
}
