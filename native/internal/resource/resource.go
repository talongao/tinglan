// Package resource is the only CDN HTTP path. Audio is streamed in bounded
// ranges to the native decoder without a local server, disk copy or credentials.
package resource

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"tinglan/native/internal/huawei"
)

const MaxChunk = 1024 * 1024

type Client struct {
	http    *http.Client
	allowed func(string) bool
}

func New() *Client { return newClient(http.DefaultTransport, huawei.SafeResourceURL) }
func newClient(t http.RoundTripper, allowed func(string) bool) *Client {
	c := &Client{allowed: allowed}
	c.http = &http.Client{Transport: t, Timeout: 25 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) >= 5 || !allowed(r.URL.String()) {
			return errors.New("resource redirect rejected")
		}
		r.Header.Del("Authorization")
		r.Header.Del("Cookie")
		return nil
	}}
	return c
}

type Chunk struct {
	Bytes []byte
	Total int64
	MIME  string
}

func (c *Client) do(ctx context.Context, raw string, headers http.Header) (*http.Response, error) {
	if !c.allowed(raw) {
		return nil, errors.New("resource URL rejected")
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, errors.New("invalid resource request")
	}
	r.Header = headers
	r.Header.Set("Accept-Encoding", "identity")
	response, err := c.http.Do(r)
	if err != nil {
		return nil, errors.New("resource request failed")
	}
	return response, nil
}
func (c *Client) Range(ctx context.Context, raw string, offset int64, length int) (Chunk, error) {
	if offset < 0 || offset > 1<<40 || length <= 0 || length > MaxChunk {
		return Chunk{}, errors.New("invalid resource range")
	}
	r, err := c.do(ctx, raw, http.Header{"Range": {fmt.Sprintf("bytes=%d-%d", offset, offset+int64(length)-1)}})
	if err != nil {
		return Chunk{}, err
	}
	defer r.Body.Close()
	var total int64
	if r.StatusCode == http.StatusPartialContent {
		var start, end int64
		if _, err = fmt.Sscanf(r.Header.Get("Content-Range"), "bytes %d-%d/%d", &start, &end, &total); err != nil || start != offset || end < start || end >= total || end-start+1 > int64(length) {
			return Chunk{}, errors.New("invalid audio range response")
		}
		data, err := io.ReadAll(io.LimitReader(r.Body, int64(length)+1))
		if err != nil || int64(len(data)) != end-start+1 {
			return Chunk{}, errors.New("truncated audio range")
		}
		return Chunk{data, total, r.Header.Get("Content-Type")}, nil
	}
	if r.StatusCode != 200 || offset != 0 || r.ContentLength <= 0 {
		return Chunk{}, errors.New("server does not support requested audio range")
	}
	// The first probe may be served as 200; never retain the entire song.
	data, err := io.ReadAll(io.LimitReader(r.Body, min(int64(length), r.ContentLength)))
	if err != nil || int64(len(data)) != min(int64(length), r.ContentLength) {
		return Chunk{}, errors.New("audio read failed")
	}
	return Chunk{data, r.ContentLength, r.Header.Get("Content-Type")}, nil
}
func (c *Client) Bytes(ctx context.Context, raw string, limit int64) ([]byte, error) {
	if limit <= 0 || limit > 8*1024*1024 {
		return nil, errors.New("invalid resource limit")
	}
	r, err := c.do(ctx, raw, make(http.Header))
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return nil, errors.New("resource HTTP " + strconv.Itoa(r.StatusCode))
	}
	if strings.Contains(r.Header.Get("Content-Type"), "text/html") {
		return nil, errors.New("resource type rejected")
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, errors.New("resource exceeds limit")
	}
	return data, nil
}
