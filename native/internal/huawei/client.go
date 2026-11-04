package huawei

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const maxResponseBytes = 2 * 1024 * 1024

type Client struct{ http *http.Client }

// NewClient uses normal system TLS verification and process proxy settings.
// Redirects are always rejected, including those returned during token exchange.
func NewClient() *Client { return newClient(http.DefaultTransport) }

func newClient(transport http.RoundTripper) *Client {
	return &Client{http: &http.Client{Transport: transport, Timeout: 25 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirect rejected") }}}
}

// Session exposes credentials only through this protocol package. JSON and fmt
// output intentionally omit them; persistent secure storage will own encoding.
type Session struct{ token, snsUserID, expireTime string }

func (Session) String() string   { return "[music session]" }
func (Session) GoString() string { return "[music session]" }

type envelope struct {
	Result *struct {
		Code    string `json:"resultCode"`
		Message string `json:"resultMessage"`
	} `json:"result"`
	Error json.RawMessage `json:"error"`
}

// ProviderError contains structural fields only. Never retain an HTTP error
// string: net/http includes the requested URL, which can carry credentials.
type ProviderError struct {
	Stage         string
	Status        int
	Code          string
	Authenticated bool
	Expired       bool
}

func (e *ProviderError) Error() string { return fmt.Sprintf("%s failed (HTTP %d)", e.Stage, e.Status) }

func (c *Client) post(ctx context.Context, endpoint, stage, contentType string, body []byte, headers http.Header, out any, music bool) error {
	return c.request(ctx, http.MethodPost, endpoint, stage, contentType, body, headers, out, music)
}
func (c *Client) request(ctx context.Context, method, endpoint, stage, contentType string, body []byte, headers http.Header, out any, music bool) error {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return errors.New("request construction failed")
	}
	req.Header = headers
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	req.Header.Set("Content-Type", contentType)
	response, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s network request failed", stage)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	failure := &ProviderError{Stage: stage, Status: response.StatusCode, Authenticated: music && strings.Contains(headers.Get("Authorization"), ", token=")}
	var result envelope
	if json.Unmarshal(data, &result) == nil && result.Result != nil {
		failure.Code = result.Result.Code
		message := strings.ToLower(result.Result.Message)
		failure.Expired = result.Result.Code != "000000" && (strings.Contains(message, "token expired") || strings.Contains(message, "token has expired") || strings.Contains(message, "session expired") || strings.Contains(message, "token已过期") || strings.Contains(message, "令牌已过期") || strings.Contains(message, "登录已过期") || strings.Contains(message, "会话已过期"))
	}
	if err != nil || len(data) > maxResponseBytes || response.StatusCode < 200 || response.StatusCode >= 300 {
		return failure
	}
	if json.Unmarshal(data, &result) != nil || len(result.Error) != 0 {
		return failure
	}
	if music && (result.Result == nil || result.Result.Code != "000000") {
		if result.Result != nil {
			failure.Code = result.Result.Code
		}
		return failure
	}
	if json.Unmarshal(data, out) != nil {
		return failure
	}
	return nil
}

func musicHeaders(token string, now time.Time) (http.Header, error) {
	if strings.ContainsAny(token, "\r\n,") {
		return nil, errors.New("invalid music session")
	}
	value := "Digest username=app-music-baseline, nonce=" + randomString(24) + ", created=" + now.UTC().Format("2006-01-02T15:04:05Z")
	if token != "" {
		value += ", token=" + token
	}
	return http.Header{"Authorization": {value}}, nil
}

// Exchange never returns the account access/refresh tokens to the application.
// The candidate must pass ValidateSession before replacing a current session.
func (c *Client) Exchange(ctx context.Context, attempt *Attempt) (*Session, error) {
	form, err := attempt.exchangeForm(time.Now())
	if err != nil {
		return nil, err
	}
	var tokens struct {
		AccessToken string `json:"access_token"`
	}
	if err := c.post(ctx, TokenEndpoint, "code exchange", "application/x-www-form-urlencoded", []byte(form.Encode()), nil, &tokens, false); err != nil {
		return nil, err
	}
	if tokens.AccessToken == "" {
		return nil, errors.New("account access token missing")
	}
	body, _ := json.Marshal(struct {
		AccessToken string `json:"accessToken"`
	}{tokens.AccessToken})
	headers, _ := musicHeaders("", time.Now())
	var result struct {
		Token  string `json:"token"`
		UserID string `json:"snsUserID"`
		Expiry string `json:"expireTime"`
	}
	if err := c.post(ctx, MusicEndpoint+"/music-odpapp-service/v5/service/userinfo/loginex", "music exchange", "application/json", body, headers, &result, true); err != nil {
		return nil, err
	}
	if result.Token == "" || result.UserID == "" {
		return nil, errors.New("music session incomplete")
	}
	if _, err := musicHeaders(result.Token, time.Now()); err != nil {
		return nil, err
	}
	return &Session{token: result.Token, snsUserID: result.UserID, expireTime: result.Expiry}, nil
}

// ValidateSession asks the service for the caller's own favorites. Naming a
// target user would let an expired token answer 200 with an empty list, which
// is exactly the failure this check exists to catch.
func (c *Client) ValidateSession(ctx context.Context, session *Session) error {
	if session == nil || session.token == "" || session.snsUserID == "" {
		return errors.New("music session missing")
	}
	body, _ := json.Marshal(struct {
		Start int   `json:"start"`
		Limit int   `json:"limit"`
		Types []int `json:"contentTypes"`
	}{0, 1, []int{1}})
	headers, err := musicHeaders(session.token, time.Now())
	if err != nil {
		return err
	}
	var result envelope
	return c.post(ctx, MusicEndpoint+"/music-userbehavior-service/v3/service/favorite/simple/all", "session validation", "application/json", body, headers, &result, true)
}

// Public metadata and CDN failures are never evidence of account expiry.
func SessionExpired(err error) bool {
	var p *ProviderError
	return errors.As(err, &p) && p.Authenticated && (p.Status == 401 || p.Expired)
}
