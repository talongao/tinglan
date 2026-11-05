//go:build !windows && (!darwin || !cgo)

package webauth

import (
	"context"
)

func Authorize(ctx context.Context, flow Flow) (string, error) {
	return ChromiumAuthorize(ctx, attempt)
}
func NeedsManualCallback() bool { return false }
