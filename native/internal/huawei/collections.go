package huawei

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type Playlist struct {
	ID, Title, Kind, Description, CoverURL string
	Count                                  *float64
}

func (c *Client) Playlists(ctx context.Context, s *Session) ([]Playlist, error) {
	if s == nil {
		return nil, errors.New("music session missing")
	}
	var result []Playlist
	// Only playlist-like collections: including contentType 1 (songs) made the
	// response exceed the 2 MiB body cap and failed the whole request. The
	// playlist-only payload stays small, so the large page size is safe.
	pageSize := 999
	seen := make(map[string]bool)
	for start := 0; start < 1000000; {
		body, _ := json.Marshal(struct {
			UserID string `json:"targetSnsUserID"`
			Start  int    `json:"start"`
			Limit  int    `json:"limit"`
			Types  []int  `json:"contentTypes"`
		}{s.snsUserID, start, pageSize, []int{4, 5, 7}})
		headers, err := musicHeaders(s.token, time.Now())
		if err != nil {
			return nil, err
		}
		var response struct {
			Items []json.RawMessage `json:"favoriteInfos"`
		}
		if err = c.post(ctx, MusicEndpoint+"/music-userbehavior-service/v3/service/favorite/simple/all", "playlists", "application/json", body, headers, &response, true); err != nil {
			var failure *ProviderError
			if pageSize > 50 && errors.As(err, &failure) && failure.Status == http.StatusBadRequest && !SessionExpired(err) {
				pageSize = 50
				continue
			}
			return nil, err
		}
		newIDs := 0
		for _, raw := range response.Items {
			var p struct {
				ID          string          `json:"contentID"`
				Title       string          `json:"contentName"`
				Kind        json.RawMessage `json:"contentType"`
				Count       json.RawMessage `json:"totalCount"`
				Description string          `json:"listDesc"`
				Picture     struct {
					Big    string `json:"bigImgURL"`
					Middle string `json:"middleImgURL"`
					Small  string `json:"smallImgURL"`
				} `json:"picture"`
			}
			if json.Unmarshal(raw, &p) != nil || p.ID == "" {
				continue
			}
			kind := number(p.Kind)
			if kind == nil {
				continue
			}
			key := strconv.Itoa(int(*kind)) + ":" + p.ID
			if seen[key] {
				continue
			}
			seen[key] = true
			newIDs++
			name := map[int]string{4: "collected", 5: "owned", 7: "chart"}[int(*kind)]
			if name != "" && p.Title != "" {
				cover := ""
				for _, candidate := range []string{p.Picture.Big, p.Picture.Middle, p.Picture.Small} {
					if SafeResourceURL(candidate) {
						cover = candidate
						break
					}
				}
				result = append(result, Playlist{CoverURL: cover, ID: p.ID, Title: p.Title, Kind: name, Description: p.Description, Count: number(p.Count)})
			}
		}
		if len(response.Items) < pageSize {
			return result, nil
		}
		if newIDs == 0 {
			return nil, errors.New("playlist pagination made no progress")
		}
		start += len(response.Items)
	}
	return nil, errors.New("playlist scan limit exceeded")
}

func (c *Client) PlaylistDetail(ctx context.Context, s *Session, p Playlist, start, limit int) (Page, error) {
	if err := pagination(start, limit); err != nil {
		return Page{}, err
	}
	if p.ID == "" || len(p.ID) > 200 {
		return Page{}, errors.New("invalid playlist ID")
	}
	if p.Kind == "owned" {
		if s == nil {
			return Page{}, errors.New("music session missing")
		}
		headers, err := musicHeaders(s.token, time.Now())
		if err != nil {
			return Page{}, err
		}
		params := url.Values{"playListID": {p.ID}, "start": {strconv.Itoa(start)}, "limit": {strconv.Itoa(limit)}}
		var response struct {
			Detail struct {
				Items []json.RawMessage `json:"playListSongInfoList"`
			} `json:"playListInfoEx"`
		}
		err = c.request(ctx, http.MethodGet, MusicEndpoint+"/music-userbehavior-service/v1/service/playlist/detail/all?"+params.Encode(), "playlist detail", "application/json", nil, headers, &response, true)
		if err != nil {
			return Page{}, err
		}
		return pageFrom(response.Detail.Items, start, limit), nil
	}
	if p.Kind != "collected" {
		return Page{}, errors.New("playlist kind unsupported")
	}
	body, _ := json.Marshal(struct {
		ID        string `json:"musicListID"`
		Copyright string `json:"copyrightSwitch"`
		Start     int    `json:"start"`
		Limit     string `json:"limit"`
	}{p.ID, "1", start, strconv.Itoa(limit)})
	headers, _ := musicHeaders("", time.Now())
	var response struct {
		Detail struct {
			Items []json.RawMessage `json:"songSimpleInfos"`
		} `json:"musicListInfoEx"`
	}
	err := c.post(ctx, MusicEndpoint+"/music-operation-service/v2/service/musiclist/detail/bymusiclistid", "playlist detail", "application/json", body, headers, &response, true)
	if err != nil {
		return Page{}, err
	}
	return pageFrom(response.Detail.Items, start, limit), nil
}

func (c *Client) History(ctx context.Context, s *Session) ([]Track, error) {
	if s == nil {
		return nil, errors.New("music session missing")
	}
	headers, err := musicHeaders(s.token, time.Now())
	if err != nil {
		return nil, err
	}
	var response struct {
		Items []json.RawMessage `json:"contentSimpleInfos"`
	}
	err = c.request(ctx, http.MethodGet, MusicEndpoint+"/music-userbehavior-service/v1/service/recentlyplayed/query?contentType=1", "history", "application/json", nil, headers, &response, true)
	if err != nil {
		return nil, err
	}
	return pageFrom(response.Items, 0, 0).Tracks, nil
}
