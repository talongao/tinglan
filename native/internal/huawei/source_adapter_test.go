package huawei

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"tinglan/native/internal/music"
	"tinglan/native/internal/source"
)

func adapterWith(session *Session, transport func(*http.Request) (*http.Response, error)) *SourceAdapter {
	return NewSourceAdapter(newClient(transportFunc(transport)), func() *Session { return session })
}

func TestSourceAdapterIdentityAndCapabilities(t *testing.T) {
	adapter := adapterWith(nil, func(*http.Request) (*http.Response, error) {
		t.Fatal("capabilities must not perform network calls")
		return nil, nil
	})
	if adapter.ID() != music.SourceHuawei {
		t.Fatalf("id = %q", adapter.ID())
	}
	capabilities := adapter.Capabilities()
	if !capabilities.Import || !capabilities.Resolve || capabilities.MaxPageSize != 50 {
		t.Fatalf("capabilities = %+v", capabilities)
	}
	if len(capabilities.Collections) != 1 || capabilities.Collections[0].Kind != "favorites" {
		t.Fatalf("collections = %+v", capabilities.Collections)
	}
}

func TestSourceAdapterLoginIsLocal(t *testing.T) {
	adapter := adapterWith(nil, func(*http.Request) (*http.Response, error) {
		t.Fatal("login must not perform network calls")
		return nil, nil
	})
	state, err := adapter.Login(context.Background())
	if err != nil || state.LoggedIn {
		t.Fatalf("signed-out login = %+v, %v", state, err)
	}

	signedIn := adapterWith(&Session{token: "t", snsUserID: "u"}, func(*http.Request) (*http.Response, error) {
		t.Fatal("login must not perform network calls")
		return nil, nil
	})
	state, err = signedIn.Login(context.Background())
	if err != nil || !state.LoggedIn || state.Account == "" {
		t.Fatalf("signed-in login = %+v, %v", state, err)
	}
}

func TestSourceAdapterImportMapsCursorAndNormalizes(t *testing.T) {
	var seenStart, seenLimit string
	adapter := adapterWith(&Session{token: "t", snsUserID: "owner"}, func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		var wire map[string]json.RawMessage
		json.Unmarshal(body, &wire)
		seenStart, seenLimit = string(wire["start"]), string(wire["limit"])
		return response(`{"result":{"resultCode":"000000"},"favoriteInfos":[
			{"contentID":"h1","contentName":"晴天 (Live)","artistNamePairs":[{"artistID":"a1","artistName":"周杰伦"}],"duration":"269","albumNamePair":{"id":"al","name":"叶惠美"},"picture":{"bigImgURL":"https://music.dbankcdn.cn/c.jpg"}},
			{"contentID":"h2","contentName":"稻香","artistName":"周杰伦","duration":"223"}
		]}`, 200), nil
	})
	page, err := adapter.Import(context.Background(), source.ImportRequest{
		Collection: source.Collection{Kind: "favorites"}, Cursor: "50", Limit: 100,
	})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if seenStart != "50" || seenLimit != "50" {
		t.Fatalf("cursor/limit = %s/%s, want 50/50 (clamped)", seenStart, seenLimit)
	}
	if len(page.Tracks) != 2 || page.Next != "" || !page.Done() {
		t.Fatalf("page = %+v", page)
	}
	first := page.Tracks[0]
	if first.Source != music.SourceHuawei || first.ID != "h1" || first.Title != "晴天 (Live)" {
		t.Fatalf("track = %+v", first)
	}
	if !first.Version.Live {
		t.Fatalf("version marker lost: %+v", first.Version)
	}
	if first.ArtistText() != "周杰伦" || first.Album == nil || first.Album.ID != "al" {
		t.Fatalf("metadata = %+v", first)
	}
	if first.Duration == nil || *first.Duration != 269 {
		t.Fatalf("duration = %v", first.Duration)
	}
	if first.CoverURL != "https://music.dbankcdn.cn/c.jpg" {
		t.Fatalf("cover = %q", first.CoverURL)
	}
}

func TestSourceAdapterImportReportsNextPage(t *testing.T) {
	adapter := adapterWith(&Session{token: "t", snsUserID: "owner"}, func(*http.Request) (*http.Response, error) {
		return response(`{"result":{"resultCode":"000000"},"favoriteInfos":[{"contentID":"h1","contentName":"Song"}]}`, 200), nil
	})
	page, err := adapter.Import(context.Background(), source.ImportRequest{Limit: 1})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if page.Next != "1" || page.Done() {
		t.Fatalf("expected next cursor 1, got %+v", page)
	}
}

func TestSourceAdapterImportRejectsBadCursorAndMissingSession(t *testing.T) {
	adapter := adapterWith(&Session{token: "t", snsUserID: "owner"}, func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid cursor must not reach the network")
		return nil, nil
	})
	if _, err := adapter.Import(context.Background(), source.ImportRequest{Cursor: "abc"}); source.KindOf(err) != source.KindProtocol {
		t.Fatalf("cursor error kind = %v (%v)", source.KindOf(err), err)
	}
	if _, err := adapter.Import(context.Background(), source.ImportRequest{Cursor: "-1"}); source.KindOf(err) != source.KindProtocol {
		t.Fatalf("negative cursor kind = %v", source.KindOf(err))
	}

	signedOut := adapterWith(nil, func(*http.Request) (*http.Response, error) {
		t.Fatal("signed-out import must not reach the network")
		return nil, nil
	})
	if _, err := signedOut.Import(context.Background(), source.ImportRequest{}); source.KindOf(err) != source.KindLogin {
		t.Fatalf("signed-out import kind = %v", source.KindOf(err))
	}
}

func TestSourceAdapterResolveClassifiesAccess(t *testing.T) {
	cases := []struct {
		name     string
		expected float64
		resolved string
		access   music.Access
		evidence string
	}{
		{"full", 258, `"258"`, music.AccessFull, "duration-match"},
		{"preview", 258, `"30"`, music.AccessPreview, "duration-short"},
		{"unknown", 258, `"240"`, music.AccessUnknown, "duration-inconclusive"},
		{"missing", 258, `""`, music.AccessUnknown, "provider-no-duration"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			adapter := adapterWith(&Session{token: "t", snsUserID: "u"}, func(*http.Request) (*http.Response, error) {
				return response(`{"result":{"resultCode":"000000"},"fileURL":"https://audio.dbankcdn.cn/a","playQuality":"1","type":"audio/mpeg","songInfo":{"duration":`+tc.resolved+`}}`, 200), nil
			})
			expected := tc.expected
			playback, err := adapter.Resolve(context.Background(), source.ResolveRequest{
				Track: music.Track{Source: music.SourceHuawei, ID: "h1", Title: "Song", Duration: &expected},
			})
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}
			if playback.Access != tc.access || playback.Evidence != tc.evidence {
				t.Fatalf("access = %v/%q, want %v/%q", playback.Access, playback.Evidence, tc.access, tc.evidence)
			}
			if playback.URL != "https://audio.dbankcdn.cn/a" || playback.Quality != "standard" {
				t.Fatalf("playback = %+v", playback)
			}
		})
	}
}

func TestSourceAdapterResolveRejectsForeignSourceAndQuality(t *testing.T) {
	adapter := adapterWith(&Session{token: "t", snsUserID: "u"}, func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid requests must not reach the network")
		return nil, nil
	})
	if _, err := adapter.Resolve(context.Background(), source.ResolveRequest{Track: music.Track{Source: music.SourceNetEase, ID: "n1"}}); source.KindOf(err) != source.KindUnsupported {
		t.Fatalf("foreign source kind = %v", source.KindOf(err))
	}
	if _, err := adapter.Resolve(context.Background(), source.ResolveRequest{Track: music.Track{Source: music.SourceHuawei, ID: "h1"}, Quality: "hires"}); source.KindOf(err) != source.KindUnsupported {
		t.Fatalf("quality kind = %v", source.KindOf(err))
	}
}

func TestClassifyProviderFailures(t *testing.T) {
	cases := []struct {
		name string
		err  error
		kind source.Kind
	}{
		{"expired", &ProviderError{Stage: "favorites", Status: 200, Expired: true}, source.KindLogin},
		{"unauthorized", &ProviderError{Stage: "favorites", Status: 401}, source.KindLogin},
		{"forbidden", &ProviderError{Stage: "playback", Status: 403}, source.KindPermission},
		{"missing", &ProviderError{Stage: "playback", Status: 404}, source.KindNotFound},
		{"rate-limited", &ProviderError{Stage: "favorites", Status: 429}, source.KindRateLimit},
		{"server", &ProviderError{Stage: "favorites", Status: 503}, source.KindTransient},
		{"business", &ProviderError{Stage: "favorites", Status: 200, Code: "02000002"}, source.KindProtocol},
		{"canceled", context.Canceled, source.KindCanceled},
		{"unknown", errors.New("boom"), source.KindTransient},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if kind := source.KindOf(classify("op", tc.err)); kind != tc.kind {
				t.Fatalf("kind = %v, want %v", kind, tc.kind)
			}
		})
	}
}

func TestClassifyNeverLeaksMessageDetails(t *testing.T) {
	err := classify("resolve", errors.New("https://secret.example/audio.mp3 token=abc"))
	if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "token=") {
		t.Fatalf("classified error leaked details: %v", err)
	}
}

func TestImportRoutesPlaylistCollection(t *testing.T) {
	var gotPath string
	adapter := adapterWith(&Session{token: "t", snsUserID: "u"}, func(r *http.Request) (*http.Response, error) {
		gotPath = r.URL.Path
		return response(`{"result":{"resultCode":"000000"},"playListInfoEx":{"playListSongInfoList":[]}}`, 200), nil
	})
	if _, err := adapter.Import(context.Background(), source.ImportRequest{Collection: source.Collection{Kind: "playlist", ID: "pl1"}, Limit: 10}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotPath, "/playlist/detail/all") {
		t.Fatalf("playlist import hit %q, want playlist detail", gotPath)
	}
}
