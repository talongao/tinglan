//go:build windows

package webauth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zzl/go-win32api/v2/win32"
	"golang.org/x/sys/windows"
	"huawei-music-desktop/native/internal/huawei"
)

func TestWebViewProfileCleanupRetriesLockedFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "runtime-lock")
	if err := os.WriteFile(file, []byte("synthetic"), 0600); err != nil {
		t.Fatal(err)
	}
	path, _ := windows.UTF16PtrFromString(file)
	handle, err := windows.CreateFile(path, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	cleanupAuthProfile(dir)
	if time.Since(start) > time.Second {
		t.Error("file lock blocked authorization completion")
	}
	if _, err := os.Stat(file); err != nil {
		t.Error("fixture did not hold the file lock")
	}
	windows.CloseHandle(handle)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("profile cleanup did not retry after runtime lock released")
}

func TestWebViewCancelledBeforeLaunch(t *testing.T) {
	if NeedsManualCallback() {
		t.Fatal("Windows login must capture the callback automatically")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Authorize(ctx, huawei.NewAttempt(time.Now())); !errors.Is(err, context.Canceled) {
		t.Fatal("cancel must prevent window creation")
	}
}

func TestWebViewNavigationPolicy(t *testing.T) {
	for _, raw := range []string{"https://evil.invalid", "file:///C:/private", "hms://redirect_uri.evil?code=x", "tinglan://callback"} {
		w := &authWindow{allowed: huawei.AllowedLoginURL}
		if w.inspect(raw) || w.settled {
			t.Fatal("untrusted navigation accepted")
		}
	}
	w := &authWindow{allowed: huawei.AllowedLoginURL}
	if !w.inspect(huawei.NewAttempt(time.Now()).AuthorizeURL()) {
		t.Fatal("authorization endpoint rejected")
	}
	if w.inspect("hms://redirect_uri?code=synthetic&state=test") || !w.settled {
		t.Fatal("callback must be captured and navigation cancelled")
	}
	w.finish("replacement", errors.New("late completion"))
	if w.result != "hms://redirect_uri?code=synthetic&state=test" || w.err != nil {
		t.Fatal("first completion overwritten")
	}
}

func TestSyntheticWebViewAuthorization(t *testing.T) {
	if os.Getenv("HWMUSIC_TEST_WEBVIEW2") == "" {
		t.Skip("explicit Windows WebView2 test required")
	}
	for _, mode := range []string{"redirect", "javascript", "popup", "cancel", "close", "failed", "redirect-again"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
			defer cancel()
			attempt := huawei.NewAttempt(time.Now())
			u, _ := url.Parse(attempt.AuthorizeURL())
			callback := "hms://redirect_uri?code=synthetic&state=" + u.Query().Get("state")
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch mode {
				case "cancel":
					cancel()
				case "close":
					authWindows.Range(func(key, _ any) bool {
						win32.PostMessage(key.(win32.HWND), win32.WM_CLOSE, 0, 0)
						return true
					})
				case "javascript":
					w.Header().Set("Content-Type", "text/html")
					_, _ = w.Write([]byte(`<script>location.href="` + callback + `"</script>`))
				case "popup":
					w.Header().Set("Content-Type", "text/html")
					_, _ = w.Write([]byte(`<script>window.open("` + callback + `")</script>`))
				default:
					http.Redirect(w, r, callback, http.StatusFound)
				}
			}))
			defer server.Close()
			address := server.URL
			if mode == "failed" {
				server.Close()
			}
			got, err := webViewAuthorize(ctx, address, func(raw string) bool { return raw == address || raw == address+"/" })
			switch mode {
			case "cancel", "close":
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancel/close failed: %v", err)
				}
			case "failed":
				if err == nil || err.Error() != "authorization page failed to load" {
					t.Fatalf("navigation failure not reported: %v", err)
				}
			default:
				if err != nil || got != callback {
					t.Fatalf("automatic callback failed: %v", err)
				}
				if attempt.Accept(got, time.Now()) != nil || attempt.Accept(got, time.Now()) == nil {
					t.Fatal("callback must pass state validation exactly once")
				}
			}
		})
	}
}
