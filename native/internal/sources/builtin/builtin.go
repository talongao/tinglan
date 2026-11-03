// Package builtin wires the in-process sources the app ships with. It is the
// only place that knows which provider implements the Source contract, so the
// shell can depend on source.Source alone.
package builtin

import (
	"context"

	"tinglan/native/internal/huawei"
	"tinglan/native/internal/source"
)

// Interactive is a built-in source that can run an in-process browser sign-in.
type Interactive interface {
	source.Source
	SignIn(ctx context.Context) (*source.Session, error)
	CheckSession(ctx context.Context, session *source.Session) (*source.Session, error)
}

// Huawei returns the built-in Huawei Music source.
func Huawei(session func() *source.Session) Interactive {
	return huawei.NewSourceAdapter(huawei.NewClient(), session)
}
