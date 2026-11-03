package huawei

import (
	"context"
	"time"

	"tinglan/native/internal/music"
	"tinglan/native/internal/source"
	"tinglan/native/internal/webauth"
)

// Login runs the interactive sign-in and returns the host-visible session. The
// browser flow, the callback validation and the exchange all stay provider-side.
func (a *SourceAdapter) SignIn(ctx context.Context) (*source.Session, error) {
	attempt := NewAttempt(time.Now())
	defer attempt.Cancel()
	raw, err := webauth.Authorize(ctx, attempt)
	if err != nil {
		return nil, classify("login", err)
	}
	if err := attempt.Accept(raw, time.Now()); err != nil {
		return nil, classify("login", err)
	}
	session, err := a.client.Exchange(ctx, attempt)
	if err != nil {
		return nil, classify("login", err)
	}
	if err := a.client.ValidateSession(ctx, session); err != nil {
		return nil, classify("login", err)
	}
	return NeutralSession(session), nil
}

// Validate re-checks a stored session without exposing provider types.
func (a *SourceAdapter) CheckSession(ctx context.Context, session *source.Session) (*source.Session, error) {
	provider := ProviderSession(session)
	if provider == nil {
		return nil, source.NewError(music.SourceHuawei, "validate", source.KindLogin, nil)
	}
	if err := a.client.ValidateSession(ctx, provider); err != nil {
		return nil, classify("validate", err)
	}
	return NeutralSession(provider), nil
}
