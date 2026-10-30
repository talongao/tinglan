package webauth

import (
	"runtime"
	"testing"
)

func TestWindowsUsesDefaultBrowserEvenWithChromiumInstalled(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows browser policy")
	}
	if !NeedsManualCallback() {
		t.Fatal("Windows must open default browser, not isolated Chromium")
	}
}
