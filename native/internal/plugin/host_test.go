package plugin

import (
	"context"
	"testing"

	"tinglan/native/internal/source"
)

func TestWebViewFlowMatchesConfiguredCallback(t *testing.T) {
	flow := &webviewFlow{
		address:  "https://oauth-login.cloud.huawei.com/oauth2/v3/authorize",
		allowed:  []string{"*.huawei.com", "oauth-login.cloud.huawei.com"},
		callback: source.CallbackRule{Scheme: "hms", Host: "redirect_uri", Path: "/callback"},
	}
	if !flow.IsCallback("hms://redirect_uri/callback?code=abc") {
		t.Fatal("expected callback to match")
	}
	if flow.IsCallback("hms://other/callback") {
		t.Fatal("host mismatch must not match")
	}
	if flow.IsCallback("https://redirect_uri/callback") {
		t.Fatal("scheme mismatch must not match")
	}
	if !flow.AllowedURL("https://oauth-login.cloud.huawei.com/oauth2/v3/authorize") {
		t.Fatal("exact host must be allowed")
	}
	if !flow.AllowedURL("https://id.cloud.huawei.com/x") {
		t.Fatal("wildcard subdomain must be allowed")
	}
	if flow.AllowedURL("https://evil.example/x") {
		t.Fatal("unlisted host must be rejected")
	}
	if flow.AllowedURL("http://oauth-login.cloud.huawei.com/x") {
		t.Fatal("plain http must be rejected")
	}
}

func TestWebViewOpenRejectsUnsafeRequests(t *testing.T) {
	host := NewHost("fake", nil, nil)
	ctx := context.Background()
	cases := []struct {
		name   string
		params string
		code   int
	}{
		{"session export", `{"url":"https://a.huawei.com/x","requireSessionExport":true}`, -32004},
		{"plain http", `{"url":"http://a.huawei.com/x"}`, -32602},
		{"empty url", `{}`, -32602},
	}
	for _, tc := range cases {
		_, err := host.webviewOpen(ctx, []byte(tc.params))
		rpcErr, ok := err.(*RPCError)
		if !ok || rpcErr.Code != tc.code {
			t.Fatalf("%s: got %v, want code %d", tc.name, err, tc.code)
		}
	}
}
