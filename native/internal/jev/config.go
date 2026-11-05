// Package jev talks to the hosted TypeSafe Jev decision API.
//
// It is an optional enhancement: callers use it only for gray-zone same-song
// pairs that code similarity could not settle. Only track metadata is sent
// (title, artists, album, duration, version markers) — never audio, source
// credentials, account identifiers or signed URLs.
package jev

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"
	"time"
)

// DefaultEndpoint is the documented API base.
const DefaultEndpoint = "https://api.typesafe.ai"

// Config is the user-editable Jev configuration. The zero value is disabled.
type Config struct {
	Enabled   bool
	Endpoint  string
	APIKey    string
	Model     string
	Threshold float64
	Timeout   time.Duration
}

// DefaultThreshold is the noul probability at which two rows are treated as the
// same recording.
const DefaultThreshold = 0.80

// DefaultTimeout bounds one request.
const DefaultTimeout = 20 * time.Second

// ErrDisabled reports that the optional judge is switched off.
var ErrDisabled = errors.New("jev: disabled")

// ErrNotConfigured reports a missing endpoint, key or model.
var ErrNotConfigured = errors.New("jev: not configured")

// NormalizeEndpoint validates and canonicalizes an API base. https is the
// default and recommended scheme; plain http stays available for self-hosted
// deployments, where the operator owns the network. A base may be given as the
// service root or as an OpenAI-style /v1 (or /v1/systemone) URL.
func NormalizeEndpoint(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("jev: empty endpoint")
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("jev: invalid endpoint")
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "https" && scheme != "http" {
		return "", errors.New("jev: endpoint must use http or https")
	}
	if parsed.Host == "" || parsed.User != nil {
		return "", errors.New("jev: invalid endpoint host")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("jev: endpoint must not carry query or fragment")
	}
	path := strings.TrimRight(parsed.Path, "/")
	path = strings.TrimSuffix(path, "/v1/systemone")
	path = strings.TrimSuffix(path, "/v1")
	path = strings.TrimSuffix(path, "/systemone")
	parsed.Scheme = scheme
	parsed.Path = path
	return parsed.String(), nil
}

// InsecureEndpoint reports that the base does not use TLS, so the bearer key
// travels in clear text. The UI must warn instead of silently accepting it.
func InsecureEndpoint(endpoint string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(endpoint)), "http://")
}

// Identity is the cache/verification scope of one configuration. Generation
// changes whenever the endpoint or credential changes, so results and
// verification records from an older configuration can never be reused.
type Identity struct {
	Endpoint   string
	Credential string
	Generation string
	Model      string
	Threshold  float64
}

// IdentityOf derives the identity without exposing the raw key.
func IdentityOf(cfg Config) (Identity, error) {
	endpoint, err := NormalizeEndpoint(cfg.Endpoint)
	if err != nil {
		return Identity{}, err
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		return Identity{}, ErrNotConfigured
	}
	credential := credentialRef(cfg.APIKey)
	sum := sha256.Sum256([]byte(endpoint + "\x00" + credential + "\x00" + model))
	return Identity{
		Endpoint:   endpoint,
		Credential: credential,
		Generation: hex.EncodeToString(sum[:8]),
		Model:      model,
		Threshold:  ThresholdOf(cfg),
	}, nil
}

// credentialRef is a short fingerprint; the raw key never leaves the client.
func credentialRef(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return "none"
	}
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:8])
}

// ThresholdOf returns the configured threshold or the default when unset or
// out of range.
func ThresholdOf(cfg Config) float64 {
	if cfg.Threshold > 0 && cfg.Threshold <= 1 {
		return cfg.Threshold
	}
	return DefaultThreshold
}

// TimeoutOf returns the configured timeout or the default.
func TimeoutOf(cfg Config) time.Duration {
	if cfg.Timeout > 0 && cfg.Timeout <= 2*time.Minute {
		return cfg.Timeout
	}
	return DefaultTimeout
}

// SecretName scopes a stored API key to one endpoint, so changing the endpoint
// never reuses an old key with a new service.
func SecretName(endpoint string) string {
	normalized, err := NormalizeEndpoint(endpoint)
	if err != nil {
		normalized = endpoint
	}
	sum := sha256.Sum256([]byte(normalized))
	return "jev:" + hex.EncodeToString(sum[:8])
}
