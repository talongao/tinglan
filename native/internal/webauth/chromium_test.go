package webauth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestSyntheticBrowserRedirect(t *testing.T) {
	if os.Getenv("HWMUSIC_TEST_BROWSER") == "" {
		t.Skip("explicit synthetic browser test required")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "hms://redirect_uri?code=synthetic&state=test")
		w.WriteHeader(302)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	got, err := chromiumAuthorize(ctx, server.URL, os.Getenv("HWMUSIC_TEST_BROWSER_VISIBLE") == "")
	if err != nil || got != "hms://redirect_uri?code=synthetic&state=test" {
		t.Fatalf("synthetic redirect failed: %v", err)
	}
}

func TestOnlyAuthorizationCallbackCaptured(t *testing.T) {
	for _, test := range []struct{ method, data, want string }{
		{"Fetch.requestPaused", `{"responseHeaders":[{"name":"Location","value":"hms://redirect_uri?code=synthetic&state=test"}]}`, "hms://redirect_uri?code=synthetic&state=test"},
		{"Page.frameRequestedNavigation", `{"url":"hms://redirect_uri?code=synthetic&state=test"}`, "hms://redirect_uri?code=synthetic&state=test"},
		{"Fetch.requestPaused", `{"responseHeaders":[{"name":"Location","value":"https://evil.invalid/hms://redirect_uri"}]}`, ""},
		{"Page.frameNavigated", `{"url":"hms://redirect_uri?code=synthetic"}`, ""},
	} {
		if got := callbackFromEvent(test.method, []byte(test.data)); got != test.want {
			t.Fatal("wrong callback capture")
		}
	}
}

func TestSyntheticBrowserJavaScriptAndCancellation(t *testing.T) {
	if os.Getenv("HWMUSIC_TEST_BROWSER") == "" {
		t.Skip("explicit synthetic browser test required")
	}
	for _, cancelled := range []bool{false, true} {
		t.Run(map[bool]string{false: "javascript", true: "cancel"}[cancelled], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if cancelled {
					cancel()
					return
				}
				w.Header().Set("Content-Type", "text/html")
				_, _ = w.Write([]byte(`<script>location.href="hms://redirect_uri?code=synthetic&state=js"</script>`))
			}))
			defer server.Close()
			got, err := chromiumAuthorize(ctx, server.URL, os.Getenv("HWMUSIC_TEST_BROWSER_VISIBLE") == "")
			if cancelled {
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancel did not propagate: %v", err)
				}
			} else if err != nil || got != "hms://redirect_uri?code=synthetic&state=js" {
				t.Fatalf("JS navigation failed: %v", err)
			}
		})
	}
}

func TestChromiumCancelledBeforeLaunch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := chromiumAuthorize(ctx, "http://127.0.0.1/unused", true); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel must prevent browser launch: %v", err)
	}
}

func TestSyntheticBrowserNavigationFailure(t *testing.T) {
	if os.Getenv("HWMUSIC_TEST_BROWSER") == "" {
		t.Skip("explicit synthetic browser test required")
	}
	// Close our own listener to exercise navigation failure without external traffic.
	server := httptest.NewServer(http.NotFoundHandler())
	address := server.URL
	server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_, err := chromiumAuthorize(ctx, address, os.Getenv("HWMUSIC_TEST_BROWSER_VISIBLE") == "")
	if err == nil || err.Error() != "authorization page failed to load" {
		t.Fatalf("must report failed navigation instead of waiting on a blank page: %v", err)
	}
}
