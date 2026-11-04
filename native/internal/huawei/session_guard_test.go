package huawei

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"tinglan/native/internal/music"
)

type guardTransport struct{ calls []string }

func (g *guardTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	g.calls = append(g.calls, r.URL.Path)
	body := `{"result":{"resultCode":"000000"},"favoriteInfos":[]}`
	if len(g.calls) > 1 {
		body = `{"result":{"resultCode":"990006","resultMessage":"User Authenticate failed : User token expired!"},"favoriteInfos":[]}`
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
}

func TestFavoritesTreatsMaskedExpiryAsLogin(t *testing.T) {
	transport := &guardTransport{}
	c := &Client{http: &http.Client{Transport: transport}}
	session := &Session{token: "stale", snsUserID: "user"}
	if _, err := c.Favorites(context.Background(), session, 0, 50); err == nil {
		t.Fatal("expected an error for a masked expired session")
	} else if kind := classify("favorites", err); !strings.Contains(kind.Error(), string(music.SourceHuawei)) {
		t.Fatalf("classify = %v", kind)
	}
	if len(transport.calls) != 2 {
		t.Fatalf("calls = %v", transport.calls)
	}
}
