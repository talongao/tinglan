package huawei

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestPlaybackUsesTopLevelAudioAndDoesNotInferPreview(t *testing.T) {
	c := newClient(transportFunc(func(*http.Request) (*http.Response, error) {
		return response(`{"result":{"resultCode":"000000"},"fileURL":"https://audio.dbankcdn.cn/a","fileInfos":[{"fileURL":"https://audio.dbankcdn.cn/metadata.json"}],"playQuality":"3","type":"audio/flac","auditionTime":30000,"songInfo":{"duration":"258","contentExInfo":"{\"lyrics\":\"[00:01]Synthetic\"}"}}`, 200), nil
	}))
	p, err := c.ResolvePlayback(context.Background(), &Session{token: "fixture", snsUserID: "fixture"}, "track", "lossless")
	if err != nil || p.URL != "https://audio.dbankcdn.cn/a" || p.Quality != "lossless" || p.Duration == nil || *p.Duration != 258 || !strings.Contains(p.LyricText, "Synthetic") {
		t.Fatalf("playback failed: %v %+v", err, p)
	}
}

func TestQualityRequiresCodecEvidence(t *testing.T) {
	for _, kind := range []string{"", "audio/mpeg", "application/json"} {
		c := newClient(transportFunc(func(*http.Request) (*http.Response, error) {
			return response(`{"result":{"resultCode":"000000"},"fileURL":"https://audio.dbankcdn.cn/a","playQuality":"3","type":"`+kind+`"}`, 200), nil
		}))
		p, err := c.ResolvePlayback(context.Background(), &Session{token: "fixture", snsUserID: "fixture"}, "track", "lossless")
		if err != nil || p.Quality != "unknown" {
			t.Fatal("unsupported lossless claim")
		}
	}
}
func TestEncryptedSessionRoundTripAndTamper(t *testing.T) {
	s := &Session{token: "private-token", snsUserID: "private-user", expireTime: "20261101"}
	key := NewVaultKey()
	blob, err := SealSession(s, key)
	if err != nil || strings.Contains(string(blob), "private-") {
		t.Fatal("session not encrypted")
	}
	restored, err := OpenSession(blob, key)
	if err != nil || restored.AccountKey() != s.AccountKey() || restored.token != s.token {
		t.Fatal("restore failed")
	}
	blob[len(blob)-1] ^= 1
	if _, err := OpenSession(blob, key); err == nil {
		t.Fatal("tampered session accepted")
	}
}

func TestPlaybackRejectsDifferentTrack(t *testing.T) {
	for _, field := range []string{"contentID", "contentCode"} {
		c := newClient(transportFunc(func(*http.Request) (*http.Response, error) {
			return response(`{"result":{"resultCode":"000000"},"fileURL":"https://audio.dbankcdn.cn/a","songInfo":{"`+field+`":"different","duration":262}}`, 200), nil
		}))
		if _, err := c.ResolvePlayback(context.Background(), &Session{token: "fixture", snsUserID: "fixture"}, "expected", "standard"); err == nil {
			t.Fatal("played resource for another track")
		}
	}
}

func TestPlaybackMarksPreviewStream(t *testing.T) {
	c := newClient(transportFunc(func(*http.Request) (*http.Response, error) {
		return response(`{"result":{"resultCode":"000000"},"fileURL":"https://hw.tingmall.com/preview/01/092/1092974-MP3-64K-FTD-P.mp3","playQuality":"1","type":"audio/mpeg","songInfo":{"duration":"0"}}`, 200), nil
	}))
	p, err := c.ResolvePlayback(context.Background(), &Session{token: "fixture", snsUserID: "fixture"}, "track", "standard")
	if err != nil || !p.Preview {
		t.Fatalf("preview stream not detected: %v %+v", err, p)
	}
}

func TestPlaybackFullStreamIsNotPreview(t *testing.T) {
	c := newClient(transportFunc(func(*http.Request) (*http.Response, error) {
		return response(`{"result":{"resultCode":"000000"},"fileURL":"https://xz.tingmall.com/content/17/146/17146808-MP3-128K-FTD.mp3?sign=x","playQuality":"1","type":"audio/mpeg","auditionTime":30000,"auditionEnd":29000,"songInfo":{"duration":"302"}}`, 200), nil
	}))
	p, err := c.ResolvePlayback(context.Background(), &Session{token: "fixture", snsUserID: "fixture"}, "track", "standard")
	if err != nil || p.Preview {
		t.Fatalf("full stream flagged as preview: %v %+v", err, p)
	}
}
