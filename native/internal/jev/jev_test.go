package jev

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"tinglan/native/internal/music"
)

func TestNormalizeEndpoint(t *testing.T) {
	cases := []struct {
		raw  string
		want string
		ok   bool
	}{
		{"https://api.typesafe.ai/", "https://api.typesafe.ai", true},
		{"https://api.typesafe.ai/v1", "https://api.typesafe.ai", true},
		{"https://api.typesafe.ai/v1/systemone", "https://api.typesafe.ai", true},
		{"https://example.com/proxy/", "https://example.com/proxy", true},
		{"http://127.0.0.1:8080/v1", "http://127.0.0.1:8080", true},
		{"http://api.typesafe.ai", "http://api.typesafe.ai", true},
		{"https://user:pw@example.com", "", false},
		{"https://example.com?x=1", "", false},
		{"", "", false},
	}
	for _, tc := range cases {
		got, err := NormalizeEndpoint(tc.raw)
		if tc.ok && (err != nil || got != tc.want) {
			t.Fatalf("NormalizeEndpoint(%q) = %q, %v", tc.raw, got, err)
		}
		if !tc.ok && err == nil {
			t.Fatalf("NormalizeEndpoint(%q) accepted", tc.raw)
		}
	}
}

func TestIdentityChangesWithEndpointOrKey(t *testing.T) {
	base := Config{Enabled: true, Endpoint: "https://api.typesafe.ai", APIKey: "k1", Model: "jev-latest"}
	first, err := IdentityOf(base)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(first.Credential, "k1") || strings.Contains(first.Generation, "k1") {
		t.Fatal("identity leaks the raw key")
	}
	second, _ := IdentityOf(Config{Enabled: true, Endpoint: "https://api.typesafe.ai", APIKey: "k2", Model: "jev-latest"})
	if first.Generation == second.Generation {
		t.Fatal("key change did not change the generation")
	}
	third, _ := IdentityOf(Config{Enabled: true, Endpoint: "https://other.example.com", APIKey: "k1", Model: "jev-latest"})
	if first.Generation == third.Generation {
		t.Fatal("endpoint change did not change the generation")
	}
}

func TestNewRequiresConfig(t *testing.T) {
	if _, err := New(Config{}); !errors.Is(err, ErrDisabled) {
		t.Fatalf("err = %v", err)
	}
	if _, err := New(Config{Enabled: true, Endpoint: "https://api.typesafe.ai", Model: "jev-latest"}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("err = %v", err)
	}
	if _, err := New(Config{Enabled: true, Endpoint: "http://api.typesafe.ai", APIKey: "k", Model: "jev-latest"}); err != nil {
		t.Fatalf("plain http rejected: %v", err)
	}
}

func newServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	return server
}

func clientFor(t *testing.T, server *httptest.Server, model string) *Client {
	t.Helper()
	client, err := New(Config{Enabled: true, Endpoint: server.URL, APIKey: "secret", Model: model})
	if err != nil {
		t.Fatal(err)
	}
	// Test servers use a private certificate; the production client keeps
	// default verification because endpoints must be HTTPS.
	client.http.Transport = server.Client().Transport
	return client
}

func TestModelsList(t *testing.T) {
	server := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Method != http.MethodGet {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("auth = %q", got)
		}
		json.NewEncoder(w).Encode(map[string]any{"models": []Model{
			{Name: "jev-latest", Description: "alias", ReleaseDate: "2026-10-14"},
			{Name: "jev-preview"},
		}})
	})
	client := clientFor(t, server, "jev-latest")
	models, err := client.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0].Name != "jev-latest" || models[1].Name != "jev-preview" {
		t.Fatalf("models = %+v", models)
	}
}

func TestModelsAuthErrorIsRedacted(t *testing.T) {
	server := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	client := clientFor(t, server, "jev-latest")
	_, err := client.Models(context.Background())
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Kind != KindAuth {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), server.URL) {
		t.Fatalf("error leaks details: %v", err)
	}
}

func TestSameRecordingSendsMetadataOnly(t *testing.T) {
	duration := 299.0
	server := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/systemone" || r.Method != http.MethodPost {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["model"] != "jev-latest" {
			t.Errorf("model = %v", body["model"])
		}
		questions, _ := body["questions"].(map[string]any)
		question, _ := questions["same_recording"].(map[string]any)
		if question["type"] != "noul" {
			t.Errorf("type = %v", question["type"])
		}
		state, _ := body["state"].(map[string]any)
		raw, _ := json.Marshal(state)
		text := string(raw)
		for _, banned := range []string{"https://", "token", "account", "secret"} {
			if strings.Contains(text, banned) {
				t.Errorf("state leaks %q: %s", banned, text)
			}
		}
		json.NewEncoder(w).Encode(map[string]any{
			"model":   "jev-1.14.0",
			"answers": map[string]any{"same_recording": map[string]any{"type": "noul", "noul": 0.91}},
		})
	})
	client := clientFor(t, server, "jev-latest")
	track := music.Track{Source: music.SourceHuawei, ID: "h1", Title: "七里香", Artists: []music.Artist{{Name: "周杰伦"}}, Duration: &duration}
	score, err := client.SameRecording(context.Background(), track, track)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(score-0.91) > 1e-9 {
		t.Fatalf("score = %v", score)
	}
}

func TestSameRecordingRejectsBadAnswers(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"missing", `{"answers":{}}`},
		{"out-of-range", `{"answers":{"same_recording":{"type":"noul","noul":1.4}}}`},
		{"wrong-type", `{"answers":{"same_recording":{"type":"noul","noul":null}}}`},
	}
	for _, tc := range cases {
		server := newServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(tc.body))
		})
		client := clientFor(t, server, "jev-latest")
		if _, err := client.SameRecording(context.Background(), music.Track{Title: "a"}, music.Track{Title: "b"}); err == nil {
			t.Fatalf("%s: bad answer accepted", tc.name)
		}
	}
}

func TestSameRecordingDoesNotFollowRedirects(t *testing.T) {
	var followed bool
	server := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			followed = true
			json.NewEncoder(w).Encode(map[string]any{"answers": map[string]any{"same_recording": map[string]any{"type": "noul", "noul": 0.99}}})
			return
		}
		http.Redirect(w, r, "/redirect", http.StatusTemporaryRedirect)
	})
	client := clientFor(t, server, "jev-latest")
	if _, err := client.SameRecording(context.Background(), music.Track{Title: "a"}, music.Track{Title: "b"}); err == nil {
		t.Fatal("redirect accepted")
	}
	if followed {
		t.Fatal("redirect was followed")
	}
}

func TestSelfTestUsesInferenceEndpoint(t *testing.T) {
	called := false
	server := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.URL.Path != "/v1/systemone" || r.Method != http.MethodPost {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"answers": map[string]any{"same_recording": map[string]any{"type": "noul", "noul": 0.99}},
		})
	})
	client := clientFor(t, server, "jev-latest")
	if err := client.SelfTest(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("self test did not call the inference endpoint")
	}
	bad := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusInternalServerError)
	})
	if err := clientFor(t, bad, "jev-latest").SelfTest(context.Background()); err == nil {
		t.Fatal("server failure accepted")
	}
	if err := (&Client{}).SelfTest(context.Background()); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("empty model err = %v", err)
	}
}

func TestModelCacheIsScopedByIdentity(t *testing.T) {
	cache := NewModelCache()
	first, _ := IdentityOf(Config{Endpoint: "https://api.typesafe.ai", APIKey: "k1", Model: "jev-latest"})
	second, _ := IdentityOf(Config{Endpoint: "https://api.typesafe.ai", APIKey: "k2", Model: "jev-latest"})
	cache.Put(first, []Model{{Name: "jev-latest"}})
	if models, ok := cache.Get(first); !ok || len(models) != 1 {
		t.Fatalf("cache miss for first identity")
	}
	if _, ok := cache.Get(second); ok {
		t.Fatal("cache leaked across credential generations")
	}
}

func TestMergeKeepsManualAndCurrent(t *testing.T) {
	merged := Merge(
		[]Model{{Name: "jev-latest"}},
		[]Model{{Name: "jev-1.13.0"}},
		"jev-1.14.0",
	)
	names := make([]string, 0, len(merged))
	for _, model := range merged {
		names = append(names, model.Name)
	}
	want := []string{"jev-latest", "jev-1.13.0", "jev-1.14.0"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("merged = %v", names)
	}
	if merged[2].Description != "当前未列出" {
		t.Fatalf("current marker = %q", merged[2].Description)
	}
}

func TestMergeDedupesAcrossSources(t *testing.T) {
	merged := Merge([]Model{{Name: "jev-latest"}}, []Model{{Name: "jev-latest"}}, "jev-latest")
	if len(merged) != 1 {
		t.Fatalf("merged = %+v", merged)
	}
}

func TestNormalizeModelID(t *testing.T) {
	if _, err := NormalizeModelID("   "); err == nil {
		t.Fatal("empty accepted")
	}
	if got, err := NormalizeModelID("a/b"); err != nil || got != "a/b" {
		t.Fatalf("slash model id = %q, %v", got, err)
	}
	if got, err := NormalizeModelID("proxy?model=1"); err != nil || got != "proxy?model=1" {
		t.Fatalf("opaque model id = %q, %v", got, err)
	}
	got, err := NormalizeModelID("  JEV-1.14.0  ")
	if err != nil || got != "JEV-1.14.0" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestThresholdAndTimeoutDefaults(t *testing.T) {
	if ThresholdOf(Config{}) != DefaultThreshold {
		t.Fatal("threshold default missing")
	}
	if ThresholdOf(Config{Threshold: 1.5}) != DefaultThreshold {
		t.Fatal("invalid threshold accepted")
	}
	if ThresholdOf(Config{Threshold: 0.6}) != 0.6 {
		t.Fatal("threshold override ignored")
	}
	if TimeoutOf(Config{}) != DefaultTimeout {
		t.Fatal("timeout default missing")
	}
}

func TestSecretNameScopedByEndpoint(t *testing.T) {
	a := SecretName("https://api.typesafe.ai/")
	b := SecretName("https://api.typesafe.ai")
	if a != b {
		t.Fatalf("trailing slash changed secret scope: %q %q", a, b)
	}
	if c := SecretName("https://other.example"); c == a {
		t.Fatalf("different endpoint reused secret scope: %q", c)
	}
	if d := SecretName("http://insecure.example"); d == a {
		t.Fatalf("insecure endpoint reused https scope: %q", d)
	}
}
