package appmeta

import (
	"strings"
	"testing"
)

func TestConstantsAreSet(t *testing.T) {
	t.Parallel()
	values := map[string]string{
		"Version":     Version,
		"DisplayName": DisplayName,
		"BrandLabel":  BrandLabel,
		"BundleID":    BundleID,
		"DataDir":     DataDir,
		"License":     License,
	}
	for name, value := range values {
		if strings.TrimSpace(value) == "" {
			t.Errorf("%s is empty", name)
		}
	}
	if strings.HasPrefix(DataDir, "/") || strings.HasPrefix(DataDir, `\`) {
		t.Errorf("DataDir must be relative to the user config dir, got %q", DataDir)
	}
	if !strings.Contains(BundleID, ".") {
		t.Errorf("BundleID does not look like a reverse-DNS identifier: %q", BundleID)
	}
}

func TestInfo(t *testing.T) {
	t.Parallel()
	want := "听澜 0.0.2 (org.tinglan.unofficial.internal.viva)"
	if got := Info(); got != want {
		t.Fatalf("Info() = %q, want %q", got, want)
	}
}
