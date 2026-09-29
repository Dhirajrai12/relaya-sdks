package main

import (
	"os"
	"path/filepath"
	"testing"

	relaya "github.com/relayaa/relaya-sdks/go"
)

// A login saved with the old hosted address moves to the new one; any other address stays.
func TestLoadConfigBaseURL(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("RELAYA_CONFIG", p)
	t.Setenv("RELAYA_BASE_URL", "")
	t.Setenv("RELAYA_API_KEY", "")
	for saved, want := range map[string]string{
		legacyBaseURL:                 relaya.DefaultBaseURL,
		"":                            relaya.DefaultBaseURL,
		"https://relaya.internal/api": "https://relaya.internal/api",
	} {
		if err := os.WriteFile(p, []byte(`{"api_key":"rk","base_url":"`+saved+`"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		c, err := loadConfig()
		if err != nil {
			t.Fatal(err)
		}
		if c.BaseURL != want {
			t.Errorf("saved %q: got %q, want %q", saved, c.BaseURL, want)
		}
	}
	if got := streamURL(relaya.DefaultBaseURL, "org1"); got != "wss://api.relaya.sbs/v1/orgs/org1/stream" {
		t.Errorf("stream URL %q", got)
	}
}
