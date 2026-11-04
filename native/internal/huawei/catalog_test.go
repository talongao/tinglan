package huawei

import (
	"context"
	"io"
	"net/http"
	"testing"

	"tinglan/native/internal/music"
	"tinglan/native/internal/source"
)

func catalogAdapter(session *Session, transport func(*http.Request) (*http.Response, error)) *SourceAdapter {
	return adapterWith(session, transport)
}

func TestCatalogSearchMapsPageAndCursor(t *testing.T) {
	adapter := catalogAdapter(&Session{token: "t", snsUserID: "owner"}, func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/music-search-service/v10/service/fuzzysearch" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		return response(`{"result":{"resultCode":"000000"},"songSimpleInfos":[
			{"contentID":"h1","contentName":"晴天","artistName":"周杰伦"},
			{"contentID":"h2","contentName":"稻香","artistName":"周杰伦"}
		],"hasNextPage":true}`, 200), nil
	})
	page, err := adapter.Catalog().Search(context.Background(), source.SearchRequest{Query: "周杰伦", Limit: 2})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(page.Items) != 2 || page.Items[0].ID != "h1" || page.Items[1].Title != "稻香" {
		t.Fatalf("items = %+v", page.Items)
	}
	if !page.HasMore || page.Next != "2" {
		t.Fatalf("cursor = %q more=%v", page.Next, page.HasMore)
	}
}

func TestCatalogSearchRejectsBadCursorWithoutNetwork(t *testing.T) {
	adapter := catalogAdapter(&Session{token: "t", snsUserID: "owner"}, func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid cursor must not reach the network")
		return nil, nil
	})
	_, err := adapter.Catalog().Search(context.Background(), source.SearchRequest{Query: "x", Cursor: "nope"})
	if source.KindOf(err) != source.KindProtocol {
		t.Fatalf("kind = %v (%v)", source.KindOf(err), err)
	}
}

func TestCatalogAlbumDetailKeepsTitleAndCover(t *testing.T) {
	adapter := catalogAdapter(nil, func(r *http.Request) (*http.Response, error) {
		return response(`{"result":{"resultCode":"000000"},"albumInfoEx":{
			"albumInfo":{"albumCode":"al1","albumName":"叶惠美","picture":{"bigImgURL":"https://music.dbankcdn.cn/al.jpg"}},
			"songSimpleInfos":[{"contentID":"h1","contentName":"以父之名"}]
		}}`, 200), nil
	})
	detail, err := adapter.Catalog().AlbumDetail(context.Background(), source.EntityRequest{
		Ref: source.EntityRef{Source: music.SourceHuawei, Kind: "album", ID: "al1"}, Limit: 20,
	})
	if err != nil {
		t.Fatalf("album: %v", err)
	}
	if detail.Album.Title != "叶惠美" || detail.Album.CoverURL == "" || len(detail.Tracks) != 1 {
		t.Fatalf("detail = %+v", detail)
	}
}

func TestCatalogArtistDetailDeclaresTruncation(t *testing.T) {
	adapter := catalogAdapter(nil, func(*http.Request) (*http.Response, error) {
		return response(`{"result":{"resultCode":"000000"},"artistInfoEx":{
			"artistInfo":{"artistName":"周杰伦","picture":{"bigImgURL":"https://music.dbankcdn.cn/a.jpg"}},
			"songSimpleInfos":[{"contentID":"h1","contentName":"晴天"}],
			"albumSimpleInfos":[{"albumCode":"al1","albumName":"叶惠美"}]
		}}`, 200), nil
	})
	detail, err := adapter.Catalog().ArtistDetail(context.Background(), source.EntityRequest{
		Ref: source.EntityRef{Source: music.SourceHuawei, Kind: "artist", ID: "ar1"},
	})
	if err != nil {
		t.Fatalf("artist: %v", err)
	}
	if !detail.Truncated || detail.Artist.Name != "周杰伦" || len(detail.Albums) != 1 {
		t.Fatalf("detail = %+v", detail)
	}
}

func TestCatalogSessionMethodsReportLoginKind(t *testing.T) {
	adapter := catalogAdapter(nil, func(*http.Request) (*http.Response, error) {
		t.Fatal("signed-out calls must not reach the network")
		return nil, nil
	})
	if _, err := adapter.Catalog().Playlists(context.Background(), source.PlaylistRequest{}); source.KindOf(err) != source.KindLogin {
		t.Fatalf("playlists kind = %v (%v)", source.KindOf(err), err)
	}
	if _, err := adapter.Catalog().PlaylistDetail(context.Background(), source.EntityRequest{Ref: source.EntityRef{ID: "p1"}}); source.KindOf(err) != source.KindLogin {
		t.Fatalf("playlist detail kind = %v (%v)", source.KindOf(err), err)
	}
	if _, err := adapter.Catalog().History(context.Background(), source.PageRequest{}); source.KindOf(err) != source.KindLogin {
		t.Fatalf("history kind = %v (%v)", source.KindOf(err), err)
	}
}

func TestManageReportsLoginWithoutSession(t *testing.T) {
	adapter := catalogAdapter(nil, func(*http.Request) (*http.Response, error) {
		t.Fatal("manage must not reach the network without a session")
		return nil, nil
	})
	manage := adapter.Manage()
	if err := manage.ReportHistory(context.Background(), music.Track{Source: music.SourceHuawei, ID: "h1"}, 30); source.KindOf(err) != source.KindLogin {
		t.Fatalf("history report kind = %v (%v)", source.KindOf(err), err)
	}
	ref := source.EntityRef{Source: music.SourceHuawei, Kind: "playlist", ID: "p1"}
	if err := manage.UpdatePlaylistDescription(context.Background(), ref, "x"); source.KindOf(err) != source.KindLogin {
		t.Fatalf("description kind = %v (%v)", source.KindOf(err), err)
	}
	if err := manage.DeletePlaylist(context.Background(), ref); source.KindOf(err) != source.KindLogin {
		t.Fatalf("delete kind = %v (%v)", source.KindOf(err), err)
	}
}

func TestAuthVerifyClassifiesExpiry(t *testing.T) {
	adapter := catalogAdapter(&Session{token: "t", snsUserID: "owner"}, func(*http.Request) (*http.Response, error) {
		return response(`{"result":{"resultCode":"000000","resultMessage":"token expired"}}`, 401), nil
	})
	status, err := adapter.Auth().Verify(context.Background())
	if err != nil || status.State != source.AuthExpired {
		t.Fatalf("verify = %+v, %v", status, err)
	}
}

func TestAuthStatusStaysLocal(t *testing.T) {
	adapter := catalogAdapter(nil, func(*http.Request) (*http.Response, error) {
		t.Fatal("status must not perform network calls")
		return nil, nil
	})
	status, err := adapter.Auth().Status(context.Background())
	if err != nil || status.State != source.AuthSignedOut {
		t.Fatalf("status = %+v, %v", status, err)
	}
	signedIn := catalogAdapter(&Session{token: "t", snsUserID: "owner"}, nil)
	status, err = signedIn.Auth().Status(context.Background())
	if err != nil || status.State != source.AuthSignedIn || status.AccountKey == "" {
		t.Fatalf("status = %+v, %v", status, err)
	}
	unknown := catalogAdapter(&Session{token: "t"}, nil)
	status, _ = unknown.Auth().Status(context.Background())
	if status.State != source.AuthUnknown {
		t.Fatalf("status = %+v", status)
	}
}

func TestLyricsNeedsASession(t *testing.T) {
	adapter := catalogAdapter(nil, func(*http.Request) (*http.Response, error) {
		t.Fatal("lyrics must not reach the network without a session")
		return nil, nil
	})
	if _, err := adapter.Lyrics().Lyrics(context.Background(), source.TrackRef{Source: music.SourceHuawei, ID: "h1"}); source.KindOf(err) != source.KindLogin {
		t.Fatalf("lyrics kind = %v (%v)", source.KindOf(err), err)
	}
}

func TestLyricsReadsWordByWordDocument(t *testing.T) {
	adapter := catalogAdapter(&Session{token: "t", snsUserID: "owner"}, func(r *http.Request) (*http.Response, error) {
		switch {
		case r.URL.Path == "/music-play-service/v3/service/file/bycontentcode":
			return response(`{"result":{"resultCode":"000000"},"fileURL":"https://hw.tingmall.com/a.mp3","songInfo":{"contentID":"h1"},
				"lyrics":"{\"lineByLineLyric\":\"https://lyric.tingmall.com/h1.lrc\",\"wordByWordLyric\":\"https://music-pics-drcn.dbankcdn.cn/h1.hmrc\"}"}`, 200), nil
		case r.URL.Path == "/h1.hmrc":
			return response("[0,2000]<0,300>晴<300,300>天\n", 200), nil
		case r.URL.Path == "/h1.lrc":
			return response("[00:00.00]晴天\n[00:00.10]Sunny Day\n", 200), nil
		}
		t.Fatalf("unexpected path %s", r.URL.Path)
		return nil, nil
	})
	lines, err := adapter.Lyrics().Lyrics(context.Background(), source.TrackRef{Source: music.SourceHuawei, ID: "h1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || lines[0].Text != "晴天" || len(lines[0].Words) != 2 {
		t.Fatalf("lines = %+v", lines)
	}
	if lines[0].Words[1].Time != 0.3 {
		t.Fatalf("word time = %v", lines[0].Words[1].Time)
	}
}

func TestImportRejectsUnknownCollection(t *testing.T) {
	adapter := catalogAdapter(&Session{token: "t", snsUserID: "owner"}, func(*http.Request) (*http.Response, error) {
		t.Fatal("unknown collection must not reach the network")
		return nil, nil
	})
	_, err := adapter.Import(context.Background(), source.ImportRequest{Collection: source.Collection{Kind: "unknown"}})
	if err == nil {
		t.Fatal("unknown collection accepted")
	}
	_ = io.Discard
}
