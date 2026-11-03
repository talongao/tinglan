package jev

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"

	"tinglan/native/internal/music"
)

// Model is one entry from GET /v1/models.
type Model struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	ReleaseDate string `json:"release_date"`
}

// Client is a Jev API client bound to one configuration identity.
type Client struct {
	identity Identity
	key      string
	http     *http.Client
}

// New validates cfg and returns a client. The raw key is kept only in memory.
func New(cfg Config) (*Client, error) {
	if !cfg.Enabled {
		return nil, ErrDisabled
	}
	identity, err := IdentityOf(cfg)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, ErrNotConfigured
	}
	return &Client{
		identity: identity,
		key:      strings.TrimSpace(cfg.APIKey),
		http: &http.Client{
			Timeout: TimeoutOf(cfg),
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

// Identity returns the client's configuration scope.
func (c *Client) Identity() Identity { return c.identity }

// ErrorKind classifies API failures so the UI can explain them.
type ErrorKind string

const (
	KindAuth      ErrorKind = "auth"
	KindModel     ErrorKind = "model"
	KindRateLimit ErrorKind = "rate-limit"
	KindNetwork   ErrorKind = "network"
	KindProtocol  ErrorKind = "protocol"
)

// Error is a redacted API error: it never includes the key or full URL.
type Error struct {
	Kind   ErrorKind
	Status int
	Err    error
}

func (e *Error) Error() string {
	if e.Status > 0 {
		return fmt.Sprintf("jev: %s (http %d)", e.Kind, e.Status)
	}
	return fmt.Sprintf("jev: %s", e.Kind)
}
func (e *Error) Unwrap() error { return e.Err }

func classify(status int, err error) error {
	kind := KindProtocol
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		kind = KindAuth
	case status == http.StatusNotFound:
		kind = KindModel
	case status == http.StatusTooManyRequests:
		kind = KindRateLimit
	case status >= 500:
		kind = KindNetwork
	case err != nil:
		kind = KindNetwork
	}
	return &Error{Kind: kind, Status: status, Err: err}
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	if c == nil {
		return ErrNotConfigured
	}
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return &Error{Kind: KindProtocol, Err: err}
		}
		reader = bytes.NewReader(data)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.identity.Endpoint+path, reader)
	if err != nil {
		return &Error{Kind: KindProtocol, Err: err}
	}
	request.Header.Set("Authorization", "Bearer "+c.key)
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(request)
	if err != nil {
		return &Error{Kind: KindNetwork, Err: err}
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return &Error{Kind: KindNetwork, Err: err}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return classify(response.StatusCode, nil)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return &Error{Kind: KindProtocol, Err: err}
	}
	return nil
}

// Models fetches the server-side model list. Entries are aliases or versioned
// ids exactly as returned; callers must not invent ids.
func (c *Client) Models(ctx context.Context) ([]Model, error) {
	var payload struct {
		Models []Model `json:"models"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/models", nil, &payload); err != nil {
		return nil, err
	}
	return payload.Models, nil
}

// Answer is the parsed noul answer for one question.
type Answer struct {
	Model  string
	Noul   float64
	Exists bool
}

// SameRecording asks Jev for the probability that two tracks describe the same
// recording/performance version. It satisfies music.Judge.
func (c *Client) SameRecording(ctx context.Context, a, b music.Track) (float64, error) {
	if c == nil {
		return 0, ErrNotConfigured
	}
	state := map[string]any{
		"track_a": trackState(a),
		"track_b": trackState(b),
	}
	body := map[string]any{
		"model": c.identity.Model,
		"state": state,
		"questions": map[string]any{
			"same_recording": map[string]any{
				"type":         "noul",
				"instructions": "track_a and track_b are two catalog entries. Return the probability that they describe the same recording and performance version of the same song. Ignore source ids, cover art and catalog noise. A live, remix, acoustic, instrumental, cover or demo version is not the same recording as the studio version.",
			},
		},
	}
	var payload struct {
		Model   string `json:"model"`
		Answers map[string]struct {
			Type string   `json:"type"`
			Noul *float64 `json:"noul"`
		} `json:"answers"`
	}
	if err := c.do(ctx, http.MethodPost, "/v1/systemone", body, &payload); err != nil {
		return 0, err
	}
	answer, ok := payload.Answers["same_recording"]
	if !ok || answer.Noul == nil {
		return 0, &Error{Kind: KindProtocol, Err: errors.New("missing noul answer")}
	}
	score := *answer.Noul
	if math.IsNaN(score) || math.IsInf(score, 0) || score < 0 || score > 1 {
		return 0, &Error{Kind: KindProtocol, Err: errors.New("noul answer out of range")}
	}
	return score, nil
}

func trackState(track music.Track) map[string]any {
	artists := make([]string, 0, len(track.Artists))
	for _, artist := range track.Artists {
		if name := strings.TrimSpace(artist.Name); name != "" {
			artists = append(artists, name)
		}
	}
	state := map[string]any{
		"title":   track.Title,
		"artists": artists,
		"version": track.Version.Key(),
	}
	if track.Album != nil {
		state["album"] = track.Album.Title
	}
	if track.Duration != nil {
		state["duration_seconds"] = math.Round(*track.Duration)
	}
	return state
}

// SelfTest sends one small same-recording question to the configured model. It
// checks the endpoint, bearer key, model id and answer format without touching
// the user's library; only two placeholder track descriptors are sent.
func (c *Client) SelfTest(ctx context.Context) error {
	if c == nil {
		return ErrNotConfigured
	}
	if strings.TrimSpace(c.identity.Model) == "" {
		return ErrNotConfigured
	}
	duration := 180.0
	track := music.Track{
		Title:    "Tinglan configuration check",
		Artists:  []music.Artist{{Name: "Tinglan"}},
		Duration: &duration,
	}
	_, err := c.SameRecording(ctx, track, track)
	return err
}

// Judge adapts a client to music.Judge. The matcher applies the threshold
// (MatchPolicy.JevMerge); the judge only reports the probability.
type Judge struct {
	Client *Client
}

// SameRecording implements music.Judge and is only called for gray-zone pairs.
func (j Judge) SameRecording(ctx context.Context, a, b music.Track) (float64, error) {
	if j.Client == nil {
		return 0, ErrNotConfigured
	}
	return j.Client.SameRecording(ctx, a, b)
}

// ModelCache stores the last successful server list per configuration identity.
type ModelCache struct {
	entries map[string][]Model
}

// NewModelCache returns an empty cache.
func NewModelCache() *ModelCache { return &ModelCache{entries: make(map[string][]Model)} }

// Get returns the cached list and whether one exists.
func (c *ModelCache) Get(identity Identity) ([]Model, bool) {
	if c == nil {
		return nil, false
	}
	models, ok := c.entries[identity.Generation]
	return append([]Model(nil), models...), ok
}

// Put records a successful list.
func (c *ModelCache) Put(identity Identity, models []Model) {
	if c == nil {
		return
	}
	c.entries[identity.Generation] = append([]Model(nil), models...)
}

// Merge combines server, manual and current selections without dropping manual
// entries. The current model is kept even when it is not listed.
func Merge(server, manual []Model, current string) []Model {
	seen := make(map[string]bool)
	merged := make([]Model, 0, len(server)+len(manual)+1)
	add := func(model Model) {
		name := strings.TrimSpace(model.Name)
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		model.Name = name
		merged = append(merged, model)
	}
	for _, model := range server {
		add(model)
	}
	for _, model := range manual {
		add(model)
	}
	if current = strings.TrimSpace(current); current != "" && !seen[current] {
		add(Model{Name: current, Description: "当前未列出"})
	}
	return merged
}

// NormalizeModelID trims a manually entered id. Ids are opaque: no case
// folding, no URL escaping, and only control characters are rejected.
func NormalizeModelID(raw string) (string, error) {
	model := strings.TrimSpace(raw)
	if model == "" {
		return "", errors.New("jev: empty model id")
	}
	if len(model) > 200 {
		return "", errors.New("jev: model id too long")
	}
	for _, r := range model {
		if r < 0x20 || r == 0x7f {
			return "", errors.New("jev: invalid model id")
		}
	}
	return model, nil
}
