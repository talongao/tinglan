//go:build !darwin || !cgo

package webauth

import (
	"context"
	"huawei-music-desktop/native/internal/huawei"
	"runtime"
)

func Authorize(ctx context.Context, attempt *huawei.Attempt) (string, error) {
	return ChromiumAuthorize(ctx, attempt)
}
func NeedsManualCallback() bool { return runtime.GOOS == "windows" || browserBinary() == "" }
