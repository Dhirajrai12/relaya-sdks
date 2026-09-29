package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	relaya "github.com/relayaa/relaya-sdks/go"
)

// config is what `relaya login` saves, in the user's config folder, readable only by them.
type config struct {
	APIKey  string `json:"api_key"`
	BaseURL string `json:"base_url"`
	OrgID   string `json:"org_id,omitempty"`
	// ListenSecret signs what `relaya listen` forwards (Relaya-Signature). It stays
	// the same across runs, so the local app's .env can keep it.
	ListenSecret string `json:"listen_secret"`
}

func configPath() (string, error) {
	if p := os.Getenv("RELAYA_CONFIG"); p != "" {
		return p, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "relaya", "config.json"), nil
}

func loadConfig() (config, error) {
	var c config
	p, err := configPath()
	if err != nil {
		return c, err
	}
	b, err := os.ReadFile(p)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return c, err
	}
	if len(b) > 0 {
		if err := json.Unmarshal(b, &c); err != nil {
			return c, err
		}
	}
	// The environment wins, e.g. in CI.
	if k := os.Getenv("RELAYA_API_KEY"); k != "" {
		c.APIKey, c.OrgID = k, ""
	}
	if u := os.Getenv("RELAYA_BASE_URL"); u != "" {
		c.BaseURL = u
	}
	if c.BaseURL == "" {
		c.BaseURL = relaya.DefaultBaseURL
	}
	return c, nil
}

func saveConfig(c config) error {
	p, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	return os.WriteFile(p, b, 0o600)
}

// client returns an API client, or tells the user to log in.
func (c config) client() (*relaya.Client, error) {
	if c.APIKey == "" {
		return nil, errors.New("not logged in: run `relaya login` (or set RELAYA_API_KEY)")
	}
	opts := []relaya.Option{relaya.WithBaseURL(c.BaseURL)}
	if c.OrgID != "" {
		opts = append(opts, relaya.WithOrgID(c.OrgID))
	}
	return relaya.New(c.APIKey, opts...), nil
}
