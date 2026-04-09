// Package setup implements a shared, portable encoding for a
// slackers workspace bootstrap payload (client id, client secret,
// app token, optionally user token). The same encoder/decoder is
// consumed by both the CLI (`slackers setup <arg>`) and the
// internal `/setup` command so the format handling stays in one
// place.
//
// # Formats
//
// Three input shapes are supported; ParseAny autodetects which one
// it received:
//
//  1. JSON — a literal JSON object starting with `{`, e.g.
//     `{"client_id":"...","client_secret":"...","app_token":"xapp-..."}`.
//  2. Flags — a whitespace-separated argument list starting with
//     `--`, e.g. `--client-id X --client-secret Y --app-token Z`.
//  3. Hash — base64url(gzip(json)) of the same JSON, with no
//     prefix. This is the compact "share with a friend over chat"
//     form and is typically ~200 chars.
//
// # Why no prefix
//
// The hash detection falls through last in ParseAny: JSON input is
// caught by its leading `{`, flag input by its leading `-`, and
// anything else is passed to Decode. Decode first base64url-decodes
// and then tries to gunzip + JSON-unmarshal the result — any
// failure along that chain yields a clean error. No key is
// required and no prefix is needed, so every client can decode
// strings from every other client indefinitely.
//
// # Security caveat
//
// This is a compact *encoding*, not an *encryption*. Anyone with
// the hash string can extract the client secret and tokens. Treat
// the hash the same way you'd treat the raw JSON — don't paste it
// into untrusted channels. The Share My Info page's hash export
// intentionally omits the user OAuth token (xoxp-) so even a
// leaked hash can only be used to bootstrap a fresh OAuth login,
// not impersonate the sharer.
package setup

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Config is the bootstrap payload — everything a client needs to
// initialise a workspace. Fields that are empty when encoded are
// simply omitted from the JSON (and therefore from the hash too).
type Config struct {
	ClientID     string `json:"client_id,omitempty"`
	ClientSecret string `json:"client_secret,omitempty"`
	AppToken     string `json:"app_token,omitempty"`
	// UserToken is the xoxp- token for an individual user. It is
	// NOT included in the output of Share My Info; the CLI / in-app
	// /setup import flow does accept it so a user migrating their
	// own machine can round-trip every field.
	UserToken string `json:"user_token,omitempty"`
}

// IsEmpty reports whether every field in the config is blank. Used
// by the import path to decide whether there's anything to apply.
func (c Config) IsEmpty() bool {
	return c.ClientID == "" && c.ClientSecret == "" && c.AppToken == "" && c.UserToken == ""
}

// Mask returns a copy of the config with sensitive fields
// abbreviated to `first4…last4`. Used by Output-view rendering
// so the user can sanity-check what they're about to import
// without the full secrets being displayed.
func (c Config) Mask() Config {
	return Config{
		ClientID:     c.ClientID, // not secret
		ClientSecret: maskSecret(c.ClientSecret),
		AppToken:     maskSecret(c.AppToken),
		UserToken:    maskSecret(c.UserToken),
	}
}

func maskSecret(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 8 {
		return "***"
	}
	return s[:4] + "…" + s[len(s)-4:]
}

// Encode returns the base64url(gzip(json)) form of the config.
// Emits no prefix — the inverse Decode function tries the format
// directly and catches failures as "not a valid setup hash".
func Encode(c Config) (string, error) {
	raw, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("setup: marshal: %w", err)
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(raw); err != nil {
		return "", fmt.Errorf("setup: gzip write: %w", err)
	}
	if err := zw.Close(); err != nil {
		return "", fmt.Errorf("setup: gzip close: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf.Bytes()), nil
}

// Decode parses a hash-encoded config back into a Config. Any
// failure (invalid base64, corrupted gzip, bad JSON) is surfaced
// as a single "not a valid setup hash" error so callers can
// cleanly distinguish "this wasn't a hash" from a real bug.
func Decode(s string) (Config, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Config{}, errors.New("setup: empty input")
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return Config{}, fmt.Errorf("setup: invalid base64: %w", err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return Config{}, fmt.Errorf("setup: invalid gzip: %w", err)
	}
	defer zr.Close()
	var out bytes.Buffer
	if _, err := io.Copy(&out, zr); err != nil {
		return Config{}, fmt.Errorf("setup: gzip read: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(out.Bytes(), &cfg); err != nil {
		return Config{}, fmt.Errorf("setup: invalid json payload: %w", err)
	}
	return cfg, nil
}

// ParseAny detects whether input is JSON, a flags string, or a
// bare hash, and returns the resulting Config. This is the single
// entry point used by both the CLI and the internal /setup
// command so the format handling stays unified.
func ParseAny(input string) (Config, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return Config{}, errors.New("empty setup input")
	}
	if strings.HasPrefix(trimmed, "{") {
		var cfg Config
		if err := json.Unmarshal([]byte(trimmed), &cfg); err != nil {
			return cfg, fmt.Errorf("invalid setup JSON: %w", err)
		}
		return cfg, nil
	}
	if strings.HasPrefix(trimmed, "-") {
		return ParseFlags(strings.Fields(trimmed))
	}
	cfg, err := Decode(trimmed)
	if err != nil {
		return cfg, fmt.Errorf("input is neither JSON, flags, nor a valid setup hash: %w", err)
	}
	return cfg, nil
}

// ParseFlags consumes a slice of flag tokens of the form
// `--client-id X --client-secret Y --app-token Z --user-token W`
// and returns a Config. Both `--name value` and `--name=value`
// forms are accepted. Unknown flags are ignored so callers can
// pass `os.Args[1:]` directly without having to pre-filter.
func ParseFlags(args []string) (Config, error) {
	var cfg Config
	i := 0
	for i < len(args) {
		tok := args[i]
		if !strings.HasPrefix(tok, "--") {
			i++
			continue
		}
		name := strings.TrimPrefix(tok, "--")
		value := ""
		// --name=value form
		if eq := strings.Index(name, "="); eq >= 0 {
			value = name[eq+1:]
			name = name[:eq]
		} else if i+1 < len(args) {
			value = args[i+1]
			i++
		}
		i++
		switch strings.ToLower(name) {
		case "client-id", "client_id":
			cfg.ClientID = value
		case "client-secret", "client_secret":
			cfg.ClientSecret = value
		case "app-token", "app_token":
			cfg.AppToken = value
		case "user-token", "user_token":
			cfg.UserToken = value
		}
	}
	if cfg.IsEmpty() {
		return cfg, errors.New("no setup fields recognised in flag list")
	}
	return cfg, nil
}

// ToJSON returns a compact single-line JSON encoding of the config
// with empty fields omitted. Used by the Share My Info page and
// `/setup share json` output.
func (c Config) ToJSON() (string, error) {
	raw, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// ToPrettyJSON returns a multi-line indented JSON encoding. Used
// by `/config` and by Output-view rendering where horizontal real
// estate allows.
func (c Config) ToPrettyJSON() (string, error) {
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
