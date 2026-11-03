package webauth

import "time"

// Flow is the provider-specific half of a browser login. Implementations own
// the authorize URL, the callback validation, and the one-shot attempt state;
// this package only drives a window and returns the raw callback.
type Flow interface {
	AuthorizeURL() string
	Accept(raw string, now time.Time) error
	Cancel()
	IsCallback(raw string) bool
	AllowedURL(raw string) bool
}
