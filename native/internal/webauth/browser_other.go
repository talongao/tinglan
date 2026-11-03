//go:build !windows && (!darwin || !cgo)

package webauth

import (
	"context"
	"tinglan/native/internal/huawei"
)

func Authorize(ctx context.Context, attempt *huawei.Attempt) (string, error) {
	return ChromiumAuthorize(ctx, attempt)
}
func NeedsManualCallback() bool { return false }
