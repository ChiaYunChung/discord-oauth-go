package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func testClientConfig() *Config {
	return &Config{
		Guilds:  []GuildConfig{{ID: "111"}}, // LoadConfig 要求非空，這裡手動建構就沒這限制，但保持一致
		Clients: []ClientConfig{{ID: "portainer", Secret: "s3cret"}},
	}
}

func newFormRequest(t *testing.T, form url.Values) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := req.ParseForm(); err != nil {
		t.Fatalf("failed to parse form: %v", err)
	}
	return req
}

func TestAuthenticateClient_FormBody(t *testing.T) {
	cfg := testClientConfig()
	req := newFormRequest(t, url.Values{"client_id": {"portainer"}, "client_secret": {"s3cret"}})

	id, ok := authenticateClient(req, cfg)
	if !ok || id != "portainer" {
		t.Fatalf("want ok=true id=portainer, got id=%q ok=%v", id, ok)
	}
}

func TestAuthenticateClient_BasicAuth(t *testing.T) {
	cfg := testClientConfig()
	req := httptest.NewRequest(http.MethodPost, "/token", strings.NewReader("code=abc"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth("portainer", "s3cret")
	if err := req.ParseForm(); err != nil {
		t.Fatalf("failed to parse form: %v", err)
	}

	id, ok := authenticateClient(req, cfg)
	if !ok || id != "portainer" {
		t.Fatalf("want ok=true id=portainer, got id=%q ok=%v", id, ok)
	}
}

func TestAuthenticateClient_WrongSecret(t *testing.T) {
	cfg := testClientConfig()
	req := newFormRequest(t, url.Values{"client_id": {"portainer"}, "client_secret": {"wrong"}})

	if _, ok := authenticateClient(req, cfg); ok {
		t.Fatal("want ok=false for wrong secret")
	}
}

func TestAuthenticateClient_UnknownClient(t *testing.T) {
	cfg := testClientConfig()
	req := newFormRequest(t, url.Values{"client_id": {"nope"}, "client_secret": {"s3cret"}})

	if _, ok := authenticateClient(req, cfg); ok {
		t.Fatal("want ok=false for unknown client id")
	}
}

func TestAuthenticateClient_MissingCredentials(t *testing.T) {
	cfg := testClientConfig()
	req := newFormRequest(t, url.Values{})

	if _, ok := authenticateClient(req, cfg); ok {
		t.Fatal("want ok=false when no credentials are provided")
	}
}
