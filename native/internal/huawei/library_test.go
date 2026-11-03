package huawei

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestNormalizationAndResourceBoundary(t *testing.T) {
	track, ok := normalize(json.RawMessage(`{"contentID":"opaque","contentName":"Song","artistName":"Artist, With Comma","artistID":["artist"],"duration":"258","albumNamePair":{"id":"album","name":"Album"},"picture":{"bigImgURL":"https://evil.invalid/x","middleImgURL":"https://music.dbankcdn.cn/x"},"contentExInfo":"broken"}`))
	if !ok || track.Duration == nil || *track.Duration != 258 || track.ArtistText() != "Artist, With Comma" || track.Album.ID != "album" || track.CoverURL != "https://music.dbankcdn.cn/x" {
		t.Fatalf("normalization: %+v", track)
	}
	for _, raw := range []string{"file:///etc/passwd", "https://dbankcdn.cn", "https://a.dbankcdn.cn.evil/x", "https://user@a.dbankcdn.cn/x", "https://127.0.0.1/x"} {
		if SafeResourceURL(raw) {
			t.Fatal("unsafe resource accepted")
		}
	}
	for _, duration := range []string{"true", "null", `"invalid"`, "-1"} {
		if number(json.RawMessage(duration)) != nil {
			t.Fatal("unknown duration became zero")
		}
	}
}
func TestFavoritesAndSearchWireContracts(t *testing.T) {
	calls := 0
	c := newClient(transportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		body, _ := io.ReadAll(r.Body)
		var wire map[string]json.RawMessage
		json.Unmarshal(body, &wire)
		if strings.HasSuffix(r.URL.Path, "favorite/simple/all") {
			if string(wire["start"]) != "50" || string(wire["limit"]) != "50" || string(wire["contentTypes"]) != "[1]" || string(wire["targetSnsUserID"]) != `"owner"` {
				t.Fatal("favorites contract changed")
			}
			return response(`{"result":{"resultCode":"000000"},"favoriteInfos":[{"contentID":"x","contentName":"Song"},{}]}`, 200), nil
		}
		if string(wire["start"]) != `"0"` || string(wire["limit"]) != `"20"` || string(wire["queryWord"]) != `"query"` {
			t.Fatal("search contract changed")
		}
		return response(`{"result":{"resultCode":"000000"},"hasNextPage":"1","songSimpleInfos":[{"contentID":"x","contentName":"Song"}]}`, 200), nil
	}))
	p, err := c.Favorites(context.Background(), &Session{token: "token", snsUserID: "owner"}, 50, 50)
	if err != nil || p.Next != 52 || p.More || len(p.Tracks) != 1 {
		t.Fatal("raw count pagination changed")
	}
	p, err = c.Search(context.Background(), " query ", 0, 20)
	if err != nil || !p.More {
		t.Fatal("search hasNextPage ignored")
	}
	if _, err := c.Favorites(context.Background(), nil, 0, 50); err == nil || calls != 2 {
		t.Fatal("unauthenticated favorites sent")
	}
}

func TestPlaylistRoutesAndHistory(t *testing.T) {
	var paths []string
	c := newClient(transportFunc(func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.Path)
		if strings.HasSuffix(r.URL.Path, "playlist/detail/all") {
			if r.Method != "GET" || r.URL.Query().Get("playListID") != "owned-id" {
				t.Fatal("owned playlist route requires exact ID")
			}
			return response(`{"result":{"resultCode":"000000"},"playListInfoEx":{"playListSongInfoList":[{"contentID":"track","contentName":"Synthetic"}]}}`, 200), nil
		}
		if strings.HasSuffix(r.URL.Path, "bymusiclistid") {
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), `"musicListID":"public-id"`) {
				t.Fatal("public playlist request")
			}
			return response(`{"result":{"resultCode":"000000"},"musicListInfoEx":{"songSimpleInfos":[]}}`, 200), nil
		}
		if strings.HasSuffix(r.URL.Path, "recentlyplayed/query") {
			if r.URL.Query().Get("contentType") != "1" {
				t.Fatal("history content type")
			}
			return response(`{"result":{"resultCode":"000000"},"contentSimpleInfos":[]}`, 200), nil
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"contentTypes":[4,5,7]`) {
			t.Fatal("playlist discovery types")
		}
		return response(`{"result":{"resultCode":"000000"},"favoriteInfos":[{"contentID":"song","contentName":"Song","contentType":1},{"contentID":"owned-id","contentName":"Synthetic playlist","contentType":"5"}]}`, 200), nil
	}))
	session := &Session{token: "synthetic", snsUserID: "synthetic"}
	list, err := c.Playlists(context.Background(), session)
	if err != nil || len(list) != 1 || list[0].Kind != "owned" {
		t.Fatal("mixed discovery normalization")
	}
	p, err := c.PlaylistDetail(context.Background(), session, list[0], 0, 50)
	if err != nil || len(p.Tracks) != 1 {
		t.Fatal("owned detail")
	}
	if _, err := c.PlaylistDetail(context.Background(), nil, Playlist{ID: "public-id", Kind: "collected"}, 0, 50); err != nil {
		t.Fatal(err)
	}
	if _, err := c.History(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	if _, err := c.PlaylistDetail(context.Background(), session, Playlist{ID: "chart", Kind: "chart"}, 0, 50); err == nil || len(paths) != 4 {
		t.Fatal("unsupported chart sent request")
	}
}

func TestPlaylistCoverUsesAllowedFallback(t *testing.T) {
	c := newClient(transportFunc(func(r *http.Request) (*http.Response, error) {
		return response(`{"result":{"resultCode":"000000"},"favoriteInfos":[{"contentID":"p","contentName":"fixture","contentType":5,"picture":{"bigImgURL":"https://evil.invalid/private","middleImgURL":"https://music.dbankcdn.cn/fixture"}}]}`, 200), nil
	}))
	list, err := c.Playlists(context.Background(), &Session{token: "synthetic", snsUserID: "fixture"})
	if err != nil || len(list) != 1 || list[0].CoverURL != "https://music.dbankcdn.cn/fixture" {
		t.Fatal("playlist cover allowlist/fallback failed")
	}
}

func TestPlaylistDiscoveryLargePageAndDedup(t *testing.T) {
	calls := 0
	c := newClient(transportFunc(func(r *http.Request) (*http.Response, error) {
		var wire struct{ Start, Limit int }
		if err := json.NewDecoder(r.Body).Decode(&wire); err != nil {
			t.Fatal(err)
		}
		if wire.Limit != 999 || wire.Start != calls*999 {
			t.Fatal("wrong mixed pagination", wire)
		}
		calls++
		items := make([]map[string]any, 0)
		if calls == 1 {
			for i := 0; i < 998; i++ {
				items = append(items, map[string]any{"contentID": fmt.Sprint(i), "contentName": "Song", "contentType": 1})
			}
		}
		items = append(items, map[string]any{"contentID": "owned", "contentName": "Playlist", "contentType": 5})
		if calls == 2 {
			items = append(items, map[string]any{"contentID": "other", "contentName": "Other", "contentType": 4})
		}
		data, _ := json.Marshal(map[string]any{"result": map[string]string{"resultCode": "000000"}, "favoriteInfos": items})
		return response(string(data), 200), nil
	}))
	list, err := c.Playlists(context.Background(), &Session{token: "synthetic", snsUserID: "fixture"})
	if err != nil || calls != 2 || len(list) != 2 {
		t.Fatal("pagination lost or duplicated playlists", err, calls, len(list))
	}
}

func TestPlaylistDiscoveryFallsBackAfterRejectedLargePage(t *testing.T) {
	calls := 0
	c := newClient(transportFunc(func(r *http.Request) (*http.Response, error) {
		var wire struct{ Start, Limit int }
		if err := json.NewDecoder(r.Body).Decode(&wire); err != nil {
			t.Fatal(err)
		}
		calls++
		if calls == 1 {
			if wire.Limit != 999 {
				t.Fatal("unexpected initial page")
			}
			return response(`{}`, 400), nil
		}
		if wire.Limit != 50 || wire.Start != 0 {
			t.Fatal("fallback changed offset")
		}
		return response(`{"result":{"resultCode":"000000"},"favoriteInfos":[{"contentID":"p","contentName":"P","contentType":5}]}`, 200), nil
	}))
	items, err := c.Playlists(context.Background(), &Session{token: "synthetic", snsUserID: "fixture"})
	if err != nil || len(items) != 1 || calls != 2 {
		t.Fatal("fallback failed", err)
	}
}

func TestSearchPaginationEncodings(t *testing.T) {
	for _, tc := range []struct {
		flag string
		more bool
	}{{`"1"`, true}, {`1`, true}, {`true`, true}, {`"true"`, true}, {`false`, false}, {`"0"`, false}, {`null`, true}} {
		t.Run(tc.flag, func(t *testing.T) {
			c := newClient(transportFunc(func(r *http.Request) (*http.Response, error) {
				return response(fmt.Sprintf(`{"result":{"resultCode":"000000"},"hasNextPage":%s,"songSimpleInfos":[{"contentID":"a","contentName":"A"},{"contentID":"b","contentName":"B"}]}`, tc.flag), 200), nil
			}))
			page, err := c.Search(context.Background(), "query", 20, 2)
			if err != nil || page.More != tc.more || page.Next != 22 {
				t.Fatalf("pagination: %+v %v", page, err)
			}
		})
	}
}
