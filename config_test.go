package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}
	return path
}

func TestLoadConfig_Valid(t *testing.T) {
	path := writeTempConfig(t, `
guilds:
  - id: "111"
    name: "Example"
    roleIds: ["222"]
    groupName: "admin"
clients:
  - id: "portainer"
    secret: "s3cret"
    redirectURIs:
      - "https://client.example.com/cb"
`)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Guilds) != 1 || cfg.Guilds[0].ID != "111" {
		t.Fatalf("unexpected guilds: %+v", cfg.Guilds)
	}
	if cfg.Guilds[0].GroupName != "admin" {
		t.Fatalf("want groupName=admin, got %q", cfg.Guilds[0].GroupName)
	}
}

func TestLoadConfig_FileNotFound(t *testing.T) {
	if _, err := LoadConfig(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Fatal("want error for missing file, got nil")
	}
}

func TestLoadConfig_InvalidYAML(t *testing.T) {
	path := writeTempConfig(t, "guilds: [this is not valid: yaml:::")
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("want error for invalid yaml, got nil")
	}
}

func TestLoadConfig_RequiresAtLeastOneGuild(t *testing.T) {
	path := writeTempConfig(t, "guilds: []\n")
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("want error when guilds is empty, got nil")
	}
}

func TestLoadConfig_GuildMissingID(t *testing.T) {
	path := writeTempConfig(t, `
guilds:
  - name: "no id"
`)
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("want error when a guild has no id, got nil")
	}
}

func TestLoadConfig_ClientMissingSecret(t *testing.T) {
	path := writeTempConfig(t, `
guilds:
  - id: "111"
clients:
  - id: "portainer"
`)
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("want error when a client has no secret, got nil")
	}
}

func TestConfig_FindClient(t *testing.T) {
	cfg := &Config{Clients: []ClientConfig{{ID: "a", Secret: "sa"}, {ID: "b", Secret: "sb"}}}

	if c, ok := cfg.FindClient("b"); !ok || c.Secret != "sb" {
		t.Fatalf("want to find client b, got %+v ok=%v", c, ok)
	}
	if _, ok := cfg.FindClient("missing"); ok {
		t.Fatal("want ok=false for unknown client id")
	}
}

func TestClientConfig_IsRedirectURIAllowed(t *testing.T) {
	t.Run("empty allowlist permits anything", func(t *testing.T) {
		c := ClientConfig{ID: "a"}
		if !c.IsRedirectURIAllowed("https://anything.example.com") {
			t.Fatal("want true when allowlist is empty")
		}
	})

	t.Run("non-empty allowlist requires exact match", func(t *testing.T) {
		c := ClientConfig{ID: "a", RedirectURIs: []string{"https://client.example.com/cb"}}
		if !c.IsRedirectURIAllowed("https://client.example.com/cb") {
			t.Fatal("want true for an allowed uri")
		}
		if c.IsRedirectURIAllowed("https://evil.example.com") {
			t.Fatal("want false for a uri not in the allowlist")
		}
	})

	t.Run("redirect uris are scoped per client", func(t *testing.T) {
		a := ClientConfig{ID: "a", RedirectURIs: []string{"https://a.example.com/cb"}}
		b := ClientConfig{ID: "b", RedirectURIs: []string{"https://b.example.com/cb"}}
		if a.IsRedirectURIAllowed("https://b.example.com/cb") {
			t.Fatal("want client a unable to use client b's redirect_uri")
		}
		if b.IsRedirectURIAllowed("https://a.example.com/cb") {
			t.Fatal("want client b unable to use client a's redirect_uri")
		}
	})
}
