package webauth

import (
	"runtime"
	"testing"
)

func TestWindowsNeverRequestsManualCallback(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows browser policy")
	}
	if NeedsManualCallback() {
		t.Fatal("Windows login must capture the callback automatically")
	}
}
