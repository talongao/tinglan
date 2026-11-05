package huawei

import (
	"encoding/json"
	"errors"
)

// sessionState is the only representation that may cross the plugin boundary.
// It never appears in logs or UI: the host stores it in the per-plugin
// encrypted namespace.
type sessionState struct {
	Token   string `json:"token"`
	UserID  string `json:"userID"`
	Expires string `json:"expires,omitempty"`
}

// ExportSession serializes a session for the plugin transport. Callers must
// treat the result as a secret.
func ExportSession(s *Session) (string, error) {
	if s == nil || s.token == "" || s.snsUserID == "" {
		return "", errors.New("session incomplete")
	}
	raw, err := json.Marshal(sessionState{Token: s.token, UserID: s.snsUserID, Expires: s.expireTime})
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// ImportSession restores a session exported by ExportSession.
func ImportSession(raw string) (*Session, error) {
	var state sessionState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return nil, errors.New("stored session is unreadable")
	}
	if state.Token == "" || state.UserID == "" {
		return nil, errors.New("stored session is incomplete")
	}
	return &Session{token: state.Token, snsUserID: state.UserID, expireTime: state.Expires}, nil
}

// AuthorizeURL exposes the login URL for plugin hosts.
func (a *Attempt) AuthorizeURLString() string { return a.AuthorizeURL() }
