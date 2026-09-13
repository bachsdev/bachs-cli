// Package config resolves credentials and the environment they point at.
//
// The API key prefix encodes the environment — sk_sandbox_ against sandbox,
// sk_live_ against production — so there is no separate --env flag that can
// disagree with the key actually in use. Passing a sandbox key and a
// production base URL is not a combination worth supporting.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	SandboxBaseURL = "https://sandbox-api.bachs.io"
	LiveBaseURL    = "https://api.bachs.io"

	sandboxPrefix = "sk_sandbox_"
	livePrefix    = "sk_live_"
)

// ErrNoAPIKey is returned when no credential can be found anywhere.
//
// Leads with the browser flow because that is the one that does not put a key
// in shell history. The env var is listed second for CI, where there is nobody
// to approve anything.
var ErrNoAPIKey = errors.New(
	"not logged in\n" +
		"  run:  bachs login\n" +
		"  CI:   export BACHS_API_KEY=sk_sandbox_...",
)

type Config struct {
	APIKey  string
	BaseURL string
}

// IsSandbox reports whether this key targets sandbox rather than production.
func (c Config) IsSandbox() bool {
	return strings.HasPrefix(c.APIKey, sandboxPrefix)
}

// WebSocketURL is the forwarding socket for this environment.
func (c Config) WebSocketURL() string {
	ws := c.BaseURL
	ws = strings.Replace(ws, "https://", "wss://", 1)
	ws = strings.Replace(ws, "http://", "ws://", 1)
	return ws + "/v1/webhooks/listen/connect"
}

// Redacted renders the key safe to print. Never log the key itself.
func (c Config) Redacted() string {
	if len(c.APIKey) <= 16 {
		return "…"
	}
	return c.APIKey[:16] + "…"
}

func baseURLFor(apiKey string) (string, error) {
	switch {
	case strings.HasPrefix(apiKey, sandboxPrefix):
		return SandboxBaseURL, nil
	case strings.HasPrefix(apiKey, livePrefix):
		return LiveBaseURL, nil
	default:
		return "", fmt.Errorf(
			"API key must start with %s or %s — the prefix selects the environment",
			sandboxPrefix, livePrefix,
		)
	}
}

// Dir is where credentials live. Honours BACHS_CONFIG_DIR for tests and for
// anyone who keeps config somewhere unusual.
func Dir() string {
	if custom := os.Getenv("BACHS_CONFIG_DIR"); custom != "" {
		return custom
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".bachs"
	}
	return filepath.Join(home, ".config", "bachs")
}

func Path() string {
	return filepath.Join(Dir(), "config.json")
}

type stored struct {
	APIKey string `json:"api_key"`
}

// Save writes the key for later commands.
//
// 0600 in a 0700 directory. This is a plaintext secret on disk, which is worth
// being explicit about: the OS keychain would be better, and doing that across
// macOS, Linux and Windows is enough work to be its own change. Until then the
// file mode is the whole protection, so it is set deliberately rather than
// left to the process umask.
func Save(apiKey string) (string, error) {
	if _, err := baseURLFor(apiKey); err != nil {
		return "", err // reject a bad prefix before writing it anywhere
	}

	dir := Dir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("could not create %s: %w", dir, err)
	}
	// MkdirAll leaves an existing directory's mode alone, so tighten it.
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", fmt.Errorf("could not secure %s: %w", dir, err)
	}

	body, err := json.MarshalIndent(stored{APIKey: apiKey}, "", "  ")
	if err != nil {
		return "", err
	}

	// Not os.WriteFile: it follows symlinks, so a pre-planted link at this path
	// would send the key wherever it points, and it does not apply the mode to
	// an existing file, so a pre-existing 0644 config would silently stay
	// world-readable. openSecure refuses the link where the OS supports it, and
	// the explicit Chmod makes the mode hold regardless of what was there.
	path := Path()
	f, err := openSecure(path)
	if err != nil {
		return "", fmt.Errorf("could not write %s: %w", path, err)
	}
	defer f.Close()

	if err := f.Chmod(0o600); err != nil {
		return "", fmt.Errorf("could not secure %s: %w", path, err)
	}
	if _, err := f.Write(append(body, '\n')); err != nil {
		return "", fmt.Errorf("could not write %s: %w", path, err)
	}
	return path, nil
}

// Clear removes the stored credentials.
//
// Reports whether there was anything to remove, so the caller can tell "logged
// out" from "was not logged in" rather than claiming to have done something it
// did not. A missing file is not an error: running logout twice should be
// quiet, not a failure.
func Clear() (bool, error) {
	path := Path()
	err := os.Remove(path)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, os.ErrNotExist):
		return false, nil
	default:
		return false, fmt.Errorf("could not remove %s: %w", path, err)
	}
}

// Load resolves credentials in precedence order: an explicit flag, then
// BACHS_API_KEY, then the config file.
//
// The env var sits above the file so CI can run without writing one, and below
// the flag so a one-off invocation can target another account.
func Load(override string) (Config, error) {
	apiKey := override
	if apiKey == "" {
		apiKey = os.Getenv("BACHS_API_KEY")
	}

	if apiKey == "" {
		path := Path()
		raw, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				return Config{}, ErrNoAPIKey
			}
			return Config{}, fmt.Errorf("could not read %s: %w", path, err)
		}
		var s stored
		if err := json.Unmarshal(raw, &s); err != nil {
			return Config{}, fmt.Errorf("%s is not valid JSON: %w", path, err)
		}
		if s.APIKey == "" {
			return Config{}, fmt.Errorf("no api_key in %s", path)
		}
		apiKey = s.APIKey
	}

	baseURL, err := baseURLFor(apiKey)
	if err != nil {
		return Config{}, err
	}
	// An explicit override wins, for pointing at a local or staging API.
	if custom := os.Getenv("BACHS_BASE_URL"); custom != "" {
		baseURL = custom
	}

	return Config{APIKey: apiKey, BaseURL: baseURL}, nil
}
