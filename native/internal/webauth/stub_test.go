package webauth

import (
	"strings"
	"time"
)

// stubFlow is a minimal Flow for tests that exercise the browser plumbing
// without a provider: callbacks are recognised by the marker prefix and any
// URL is allowed.
type stubFlow struct {
	url      string
	accepted string
}

func (s *stubFlow) AuthorizeURL() string { return s.url }
func (s *stubFlow) Accept(raw string, _ time.Time) error {
	s.accepted = raw
	return nil
}
func (s *stubFlow) Cancel() {}
func (s *stubFlow) IsCallback(raw string) bool {
	return strings.HasPrefix(raw, "hms://") && !strings.Contains(raw, "/hms://")
}
func (s *stubFlow) AllowedURL(string) bool { return true }
