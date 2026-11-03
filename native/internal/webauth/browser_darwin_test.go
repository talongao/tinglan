//go:build darwin && cgo

package webauth

import (
	"context"
	"os"
	"testing"
	"time"

	"tinglan/native/internal/testsupport"
)

func TestMain(m *testing.M) { os.Exit(testsupport.Run(m)) }

func TestCancelledContextDoesNotOpenBrowser(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if raw, err := Authorize(ctx, &stubFlow{url: "https://login.invalid/authorize"}); err == nil || raw != "" {
		t.Fatal("cancelled authorization succeeded")
	}
}

// No NSApplication/window exists in this test process. The bridge must complete
// and dispose its cgo handle without opening a browser or making a request.
func TestMissingPresentationWindowFailsClosed(t *testing.T) {
	for range 3 {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		raw, err := Authorize(ctx, &stubFlow{url: "https://login.invalid/authorize"})
		cancel()
		if err == nil || raw != "" {
			t.Fatal("authorization without a window succeeded")
		}
	}
}
