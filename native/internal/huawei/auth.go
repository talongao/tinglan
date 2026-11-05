// Package huawei owns the provider protocol. Credentials never enter UI models.
package huawei

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	AuthorizeEndpoint = "https://oauth-login.platform.hicloud.com/oauth2/v3/authorize"
	TokenEndpoint     = "https://oauth-login.cloud.huawei.com/oauth2/v3/token"
	MusicEndpoint     = "https://api-drcn.music.dbankcloud.cn"
	clientID          = "101444301"
	redirectURI       = "hms://redirect_uri"
	attemptLifetime   = 10 * time.Minute
)

// Attempt is single-use and deliberately cannot be JSON-marshaled with secrets.
type Attempt struct {
	mu        sync.Mutex
	verifier  string
	state     string
	created   time.Time
	consumed  bool
	code      string
	exchanged bool
}

func randomString(n int) string {
	bytes := make([]byte, n)
	rand.Read(bytes)
	return base64.RawURLEncoding.EncodeToString(bytes)
}

func challenge(verifier string) string {
	digest := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func NewAttempt(now time.Time) *Attempt {
	return &Attempt{verifier: randomString(48), state: randomString(32), created: now}
}

func (a *Attempt) AuthorizeURL() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	params := url.Values{
		"client_id": {clientID}, "redirect_uri": {redirectURI},
		"response_type": {"code"}, "access_type": {"offline"}, "scope": {"openid"},
		"state": {a.state}, "code_challenge": {challenge(a.verifier)},
		"code_challenge_method": {"S256"}, "countryCode": {"cn"}, "ui_locales": {"zh-cn"},
	}
	return AuthorizeEndpoint + "?" + params.Encode()
}

func IsCallback(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && strings.EqualFold(u.Scheme, "hms") &&
		strings.EqualFold(u.Host, "redirect_uri") && u.User == nil &&
		u.Path == "" && u.Opaque == "" && !strings.Contains(raw, "#")
}

func AllowedLoginURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || (u.Port() != "" && u.Port() != "443") {
		return false
	}
	switch strings.ToLower(u.Hostname()) {
	case "oauth-login.platform.hicloud.com", "id1.cloud.huawei.com", "oauth-login.cloud.huawei.com", "account.huawei.com", "consumer.huawei.com":
		return true
	case "oauth-login1.cloud.huawei.com":
		return u.EscapedPath() == "/oauth2/v3/loginCallback"
	}
	return false
}

// Accept consumes the first exact callback, even when it is malformed. A new
// attempt is required after denial or an invalid state, just like the old host.
func (a *Attempt) Accept(raw string, now time.Time) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.consumed {
		return errors.New("authorization attempt already settled")
	}
	if !IsCallback(raw) {
		return errors.New("authorization callback destination rejected")
	}
	a.consumed = true
	if now.Before(a.created) || now.Sub(a.created) > attemptLifetime {
		return errors.New("authorization attempt expired")
	}
	u, _ := url.Parse(raw)
	params, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return errors.New("invalid authorization callback")
	}
	for _, key := range []string{"state", "code", "error"} {
		if len(params[key]) > 1 {
			return errors.New("duplicate authorization parameter")
		}
	}
	if subtle.ConstantTimeCompare([]byte(params.Get("state")), []byte(a.state)) != 1 {
		return errors.New("authorization state mismatch")
	}
	if params.Has("error") {
		return errors.New("authorization denied")
	}
	a.code = params.Get("code")
	if a.code == "" {
		return errors.New("authorization code missing")
	}
	return nil
}

func (a *Attempt) Cancel() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.consumed, a.exchanged = true, true
	a.code, a.verifier = "", ""
}

func (a *Attempt) exchangeForm(now time.Time) (url.Values, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.consumed || a.code == "" || a.exchanged || now.Before(a.created) || now.Sub(a.created) > attemptLifetime {
		return nil, errors.New("authorization code unavailable")
	}
	a.exchanged = true
	form := url.Values{"grant_type": {"authorization_code"}, "client_id": {clientID},
		"redirect_uri": {redirectURI}, "code": {a.code}, "code_verifier": {a.verifier}}
	a.code, a.verifier = "", ""
	return form, nil
}

// IsCallback reports whether raw is the redirect this attempt expects.
func (a *Attempt) IsCallback(raw string) bool { return IsCallback(raw) }

// AllowedURL reports whether a navigation stays inside the login flow.
func (a *Attempt) AllowedURL(raw string) bool { return AllowedLoginURL(raw) }
