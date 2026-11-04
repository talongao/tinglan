package huawei

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func callback(a *Attempt) string { return redirectURI + "?code=synthetic-code&state=" + a.state }

func TestPKCE(t *testing.T) {
	if challenge("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk") != "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM" {
		t.Fatal("RFC 7636 vector")
	}
	a, b := NewAttempt(time.Now()), NewAttempt(time.Now())
	if a.state == b.state || a.verifier == b.verifier {
		t.Fatal("attempts must be independent")
	}
	u, _ := url.Parse(a.AuthorizeURL())
	if u.Query().Get("scope") != "openid" || u.Query().Get("code_challenge_method") != "S256" {
		t.Fatal("unexpected grant")
	}
}

func TestCallbackRejects(t *testing.T) {
	for _, bad := range []string{"hms://redirect_uri.evil?code=x", "hms://user@redirect_uri", "hms://redirect_uri:443", "hms://redirect_uri/other", "hms://redirect_uri#", "https://redirect_uri"} {
		if IsCallback(bad) {
			t.Fatalf("accepted %s", bad)
		}
	}
	for _, suffix := range []string{"&state=duplicate", "&code=duplicate", "&error=a&error=b", "&broken=%xx"} {
		a := NewAttempt(time.Now())
		if a.Accept(callback(a)+suffix, time.Now()) == nil {
			t.Fatal("accepted malformed callback")
		}
		if a.Accept(callback(a), time.Now()) == nil {
			t.Fatal("replayed failed attempt")
		}
	}
	for _, age := range []time.Duration{-time.Second, 11 * time.Minute} {
		a := NewAttempt(time.Now().Add(-age))
		if a.Accept(callback(a), time.Now()) == nil {
			t.Fatal("accepted invalid age")
		}
	}
	a := NewAttempt(time.Now())
	if a.Accept(redirectURI+"?code=x&state=wrong", time.Now()) == nil {
		t.Fatal("accepted wrong state")
	}
	a = NewAttempt(time.Now())
	if a.Accept(redirectURI+"?error=denied&state="+a.state, time.Now()) == nil {
		t.Fatal("accepted denial")
	}
	a = NewAttempt(time.Now())
	a.Cancel()
	if a.Accept(callback(a), time.Now()) == nil {
		t.Fatal("accepted cancellation")
	}
}

func TestConcurrentCallbackOnlyConsumedOnce(t *testing.T) {
	a := NewAttempt(time.Now())
	raw := callback(a)
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() { results <- a.Accept(raw, time.Now()) })
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("successes=%d", success)
	}
}

func TestLoginAllowlist(t *testing.T) {
	for _, good := range []string{"https://account.huawei.com/", "https://oauth-login1.cloud.huawei.com:443/oauth2/v3/loginCallback"} {
		if !AllowedLoginURL(good) {
			t.Fatal("rejected observed route")
		}
	}
	for _, bad := range []string{"http://account.huawei.com", "https://user@account.huawei.com", "https://account.huawei.com:444", "https://account.huawei.com.evil", "https://oauth-login1.cloud.huawei.com/other", "https://oauth-login2.cloud.huawei.com/oauth2/v3/loginCallback"} {
		if AllowedLoginURL(bad) {
			t.Fatal("accepted untrusted navigation")
		}
	}
}

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(body string, status int) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func TestExchangeAndValidation(t *testing.T) {
	a := NewAttempt(time.Now())
	verifier := a.verifier
	if err := a.Accept(callback(a), time.Now()); err != nil {
		t.Fatal(err)
	}
	step := 0
	c := newClient(transportFunc(func(r *http.Request) (*http.Response, error) {
		step++
		body, _ := io.ReadAll(r.Body)
		switch step {
		case 1:
			form, _ := url.ParseQuery(string(body))
			if r.URL.String() != TokenEndpoint || form.Get("code_verifier") != verifier || form.Has("client_secret") {
				t.Fatal("incorrect code request")
			}
			return response(`{"access_token":"private-account-token","refresh_token":"discard"}`, 200), nil
		case 2:
			if string(body) != `{"accessToken":"private-account-token"}` || strings.Contains(r.Header.Get("Authorization"), "private-account-token") {
				t.Fatal("account token in wrong location")
			}
			return response(`{"result":{"resultCode":"000000"},"token":"private-music-token","snsUserID":"synthetic-user","expireTime":"20261101000000"}`, 200), nil
		case 3:
			if !strings.Contains(string(body), `"targetSnsUserID":"synthetic-user"`) || !strings.Contains(r.Header.Get("Authorization"), "token=private-music-token") {
				t.Fatal("incorrect validation request")
			}
			return response(`{"result":{"resultCode":"000000"},"favoriteInfos":[]}`, 200), nil
		}
		t.Fatal("unexpected retry")
		return nil, errors.New("unexpected request")
	}))
	session, err := c.Exchange(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ValidateSession(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Exchange(context.Background(), a); err == nil {
		t.Fatal("code reused")
	}
	encoded, _ := json.Marshal(session)
	if strings.Contains(string(encoded)+fmt.Sprintf("%+v %#v", session, session), "private-") {
		t.Fatal("credentials escaped")
	}
	if step != 3 {
		t.Fatal("unexpected requests")
	}
}

func TestErrorsDoNotLeakOrRetry(t *testing.T) {
	for _, fixture := range []struct {
		body   string
		status int
	}{
		{`{"error":1101,"error_description":"private-code"}`, 400},
		{`{"result":{"resultCode":"990006"},"token":"private-token"}`, 200},
		{`{"token":"private-token"}`, 200},
		{strings.Repeat("x", maxResponseBytes+1), 200},
	} {
		calls := 0
		c := newClient(transportFunc(func(*http.Request) (*http.Response, error) {
			calls++
			return response(fixture.body, fixture.status), nil
		}))
		err := c.ValidateSession(context.Background(), &Session{token: "synthetic", snsUserID: "synthetic"})
		if err == nil || strings.Contains(fmt.Sprintf("%+v", err), "private-") || calls != 1 {
			t.Fatal("unsafe error or retry")
		}
	}
	c := newClient(transportFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("private-token") }))
	err := c.ValidateSession(context.Background(), &Session{token: "synthetic", snsUserID: "synthetic"})
	if err == nil || strings.Contains(err.Error(), "private-token") {
		t.Fatal("transport error leaked")
	}
	calls := 0
	c = newClient(transportFunc(func(*http.Request) (*http.Response, error) {
		calls++
		r := response("", 302)
		r.Header.Set("Location", "https://untrusted.invalid/?code=private-code")
		return r, nil
	}))
	err = c.ValidateSession(context.Background(), &Session{token: "synthetic", snsUserID: "synthetic"})
	if err == nil || calls != 1 || strings.Contains(err.Error(), "private-code") {
		t.Fatal("redirect followed or leaked")
	}
}
