package huawei

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Artist struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name"`
}
type Album struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}
type Track struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Artists   []Artist `json:"artists"`
	Album     *Album   `json:"album,omitempty"`
	CoverURL  string   `json:"coverUrl,omitempty"`
	CoverURLs []string `json:"coverUrls,omitempty"`
	Duration  *float64 `json:"durationSeconds,omitempty"`
	// OnlySource restricts playback to one source binding. Imported playlists
	// set it so a mirrored list never falls back to another platform's stream.
	// It is queue state, not source metadata, so it stays out of API payloads.
	OnlySource string `json:"-"`
	// Collected marks a row that exists in 收录音乐, so its ID is a stable local
	// id. Transient search results keep the platform id instead.
	Collected bool `json:"-"`
}

func (t Track) ArtistText() string {
	names := make([]string, 0, len(t.Artists))
	for _, artist := range t.Artists {
		names = append(names, artist.Name)
	}
	if len(names) == 0 {
		return "未知艺人"
	}
	return strings.Join(names, " / ")
}

type Page struct {
	Tracks   []Track
	Next     int
	More     bool
	RawCount int
}

func SafeResourceURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || (u.Port() != "" && u.Port() != "443") {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, suffix := range []string{".dbankcdn.cn", ".tingmall.com", ".kuke.com"} {
		if strings.HasSuffix(host, suffix) && len(host) > len(suffix) {
			return true
		}
	}
	return false
}

// Provider fields are loose JSON, but only normalized fields leave this package.
type providerTrack struct {
	ID          string   `json:"contentID"`
	IDAlt       string   `json:"contentId"`
	Code        string   `json:"songCode"`
	Title       string   `json:"contentName"`
	SongName    string   `json:"songName"`
	ArtistName  string   `json:"artistName"`
	SingerName  string   `json:"singerName"`
	ArtistIDs   []string `json:"artistID"`
	ArtistPairs []struct {
		ID      string `json:"artistID"`
		IDAlt   string `json:"id"`
		Name    string `json:"artistName"`
		NameAlt string `json:"name"`
	} `json:"artistNamePairs"`
	AlbumID    string `json:"albumID"`
	AlbumIDAlt string `json:"albumId"`
	AlbumName  string `json:"albumName"`
	AlbumPair  struct {
		ID        string `json:"id"`
		AlbumID   string `json:"albumID"`
		Name      string `json:"name"`
		AlbumName string `json:"albumName"`
	} `json:"albumNamePair"`
	Picture struct {
		Big    string `json:"bigImgURL"`
		Middle string `json:"middleImgURL"`
		Small  string `json:"smallImgURL"`
	} `json:"picture"`
	CoverURL string          `json:"coverUrl"`
	Cover    string          `json:"cover"`
	Duration json.RawMessage `json:"duration"`
}

func first(values ...string) string {
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}
func number(raw json.RawMessage) *float64 {
	var text string
	if json.Unmarshal(raw, &text) != nil {
		text = string(raw)
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return nil
	}
	return &value
}
func normalize(raw json.RawMessage) (Track, bool) {
	var p providerTrack
	if json.Unmarshal(raw, &p) != nil {
		return Track{}, false
	}
	t := Track{ID: first(p.ID, p.IDAlt, p.Code), Title: first(p.Title, p.SongName), Duration: number(p.Duration)}
	if t.ID == "" || t.Title == "" {
		return Track{}, false
	}
	for _, pair := range p.ArtistPairs {
		name := first(pair.Name, pair.NameAlt)
		if name != "" {
			t.Artists = append(t.Artists, Artist{ID: first(pair.ID, pair.IDAlt), Name: name})
		}
	}
	if len(t.Artists) == 0 {
		if name := first(p.ArtistName, p.SingerName); name != "" {
			artist := Artist{Name: name}
			if len(p.ArtistIDs) == 1 {
				artist.ID = p.ArtistIDs[0]
			}
			t.Artists = []Artist{artist}
		}
	}
	albumID, albumName := first(p.AlbumID, p.AlbumIDAlt, p.AlbumPair.ID, p.AlbumPair.AlbumID), first(p.AlbumName, p.AlbumPair.Name, p.AlbumPair.AlbumName)
	if albumID != "" && albumName != "" {
		t.Album = &Album{ID: albumID, Title: albumName}
	}
	for _, cover := range []string{p.Picture.Big, p.Picture.Middle, p.Picture.Small, p.CoverURL, p.Cover} {
		if SafeResourceURL(cover) {
			t.CoverURLs = append(t.CoverURLs, cover)
			if t.CoverURL == "" {
				t.CoverURL = cover
			}
		}
	}
	return t, true
}

func pageFrom(items []json.RawMessage, start, limit int) Page {
	p := Page{Next: start + len(items), More: len(items) == limit, RawCount: len(items)}
	for _, raw := range items {
		if track, ok := normalize(raw); ok {
			p.Tracks = append(p.Tracks, track)
		}
	}
	return p
}
func pagination(start, limit int) error {
	if start < 0 || start > 1000000 || limit < 1 || limit > 50 {
		return errors.New("invalid pagination")
	}
	return nil
}
func (c *Client) Favorites(ctx context.Context, session *Session, start, limit int) (Page, error) {
	if err := pagination(start, limit); err != nil {
		return Page{}, err
	}
	if session == nil || session.token == "" || session.snsUserID == "" {
		return Page{}, errors.New("music session missing")
	}
	body, _ := json.Marshal(struct {
		UserID string `json:"targetSnsUserID"`
		Start  int    `json:"start"`
		Limit  int    `json:"limit"`
		Types  []int  `json:"contentTypes"`
	}{session.snsUserID, start, limit, []int{1}})
	headers, err := musicHeaders(session.token, time.Now())
	if err != nil {
		return Page{}, err
	}
	var response struct {
		Items []json.RawMessage `json:"favoriteInfos"`
	}
	err = c.post(ctx, MusicEndpoint+"/music-userbehavior-service/v3/service/favorite/simple/all", "favorites", "application/json", body, headers, &response, true)
	if err != nil {
		return Page{}, err
	}
	return pageFrom(response.Items, start, limit), nil
}
func (c *Client) Search(ctx context.Context, query string, start, limit int) (Page, error) {
	if err := pagination(start, limit); err != nil {
		return Page{}, err
	}
	query = strings.TrimSpace(query)
	if query == "" || len([]rune(query)) > 80 {
		return Page{}, errors.New("invalid search query")
	}
	body, _ := json.Marshal(struct {
		Type    string `json:"contentType"`
		Correct string `json:"correct"`
		Query   string `json:"queryWord"`
		Start   string `json:"start"`
		Limit   string `json:"limit"`
	}{"1", "1", query, strconv.Itoa(start), strconv.Itoa(limit)})
	headers, _ := musicHeaders("", time.Now())
	var response struct {
		Items []json.RawMessage `json:"songSimpleInfos"`
		Next  json.RawMessage   `json:"hasNextPage"`
	}
	err := c.post(ctx, MusicEndpoint+"/music-search-service/v10/service/fuzzysearch", "search", "application/json", body, headers, &response, true)
	if err != nil {
		return Page{}, err
	}
	p := pageFrom(response.Items, start, limit)
	// Some responses omit this field or encode it as a boolean/string.
	switch strings.TrimSpace(string(response.Next)) {
	case "true", `"true"`, "1", `"1"`:
		p.More = true
	case "false", `"false"`, "0", `"0"`:
		p.More = false
	}
	p.More = p.More && p.RawCount > 0
	return p, nil
}

// fetchText downloads a small text resource such as a lyric document. The URL
// must pass the same allowlist as audio so a provider response cannot point the
// client at an arbitrary host.
func (c *Client) fetchText(ctx context.Context, raw string, limit int64) (string, error) {
	if !SafeResourceURL(raw) {
		return "", errors.New("resource rejected")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return "", errors.New("invalid resource request")
	}
	response, err := c.http.Do(request)
	if err != nil {
		return "", errors.New("resource request failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", errors.New("resource request failed")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit))
	if err != nil {
		return "", errors.New("resource read failed")
	}
	return string(body), nil
}
