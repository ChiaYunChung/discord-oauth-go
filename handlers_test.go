package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// newTestApp 建立一個帶假 store 的 App，供 handler 測試使用。
func newTestApp(cfg *Config) *App {
	return &App{
		clientID:     "broker-client-id",
		clientSecret: "broker-client-secret",
		redirectURI:  "https://broker.example.com/callback",
		config:       cfg,
		store:        NewStore(time.Minute),
	}
}

func newTestBroker(app *App) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", handleHealthz)
	mux.HandleFunc("/authorize", app.handleClientAuthorize)
	mux.HandleFunc("/token", app.handleClientToken)
	mux.HandleFunc("/userinfo", app.handleClientUserInfo)
	mux.HandleFunc("/callback", app.handleDiscordCallback)
	return httptest.NewServer(mux)
}

// noRedirectClient 不自動跟隨 redirect，讓測試可以檢查 Location header。
func noRedirectClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// TestFullLoginFlow 模擬整條 /authorize -> Discord -> /callback -> /token ->
// /userinfo 的路徑（Discord 端用假 server 取代），驗證：
//   - session state 有正確產生並在 /callback 被消費
//   - client 原本帶的 state 有原封不動送回去
//   - /token 需要正確的 client 憑證才能換到 access_token
//   - /userinfo 能拿到正確的 sub/username/email/groups/picture
func TestFullLoginFlow_AllowedWithGroupsAndPicture(t *testing.T) {
	withMockDiscord(t, mockDiscordConfig{
		AccessToken: "discord-token-1",
		User: map[string]interface{}{
			"id": "user-1", "username": "alice", "email": "alice@example.com", "avatar": "abc123",
		},
		Guilds:       []map[string]interface{}{{"id": "guild-1"}},
		RolesByGuild: map[string][]string{"guild-1": {"role-admin"}},
	})

	cfg := &Config{
		Guilds: []GuildConfig{
			{ID: "guild-1", RoleIDs: []string{"role-admin"}, GroupName: "admin"},
		},
		AllowedRedirectURIs: []string{"https://client.example.com/cb"},
		Clients:             []ClientConfig{{ID: "test-client", Secret: "test-secret"}},
	}
	broker := newTestBroker(newTestApp(cfg))
	defer broker.Close()

	client := noRedirectClient()

	// 1) /authorize：應該 302 到 Discord，並帶上我們自己產生的 session state
	authURL := broker.URL + "/authorize?" + url.Values{
		"redirect_uri": {"https://client.example.com/cb"},
		"state":        {"client-state-xyz"},
		"client_id":    {"test-client"},
	}.Encode()
	authResp, err := client.Get(authURL)
	if err != nil {
		t.Fatalf("authorize request failed: %v", err)
	}
	if authResp.StatusCode != http.StatusFound {
		t.Fatalf("want 302 from /authorize, got %d", authResp.StatusCode)
	}
	discordLoc, err := authResp.Location()
	if err != nil {
		t.Fatalf("missing Location header: %v", err)
	}
	sessionState := discordLoc.Query().Get("state")
	if sessionState == "" {
		t.Fatal("want a session state to be embedded in the discord authorize url")
	}
	if !strings.Contains(discordLoc.Query().Get("scope"), "guilds.members.read") {
		t.Fatalf("want scope to include guilds.members.read, got %q", discordLoc.Query().Get("scope"))
	}

	// 2) /callback：模擬 Discord 帶著 code + 上面拿到的 state 導回來
	cbResp, err := client.Get(broker.URL + "/callback?code=discord-code-xyz&state=" + sessionState)
	if err != nil {
		t.Fatalf("callback request failed: %v", err)
	}
	if cbResp.StatusCode != http.StatusFound {
		body, _ := io.ReadAll(cbResp.Body)
		t.Fatalf("want 302 from /callback, got %d: %s", cbResp.StatusCode, body)
	}
	clientLoc, err := cbResp.Location()
	if err != nil {
		t.Fatalf("missing Location header: %v", err)
	}
	if !strings.HasPrefix(clientLoc.String(), "https://client.example.com/cb") {
		t.Fatalf("want redirect back to the client's redirect_uri, got %s", clientLoc)
	}
	if clientLoc.Query().Get("state") != "client-state-xyz" {
		t.Fatalf("want the original client state to be preserved, got %q", clientLoc.Query().Get("state"))
	}
	issuedCode := clientLoc.Query().Get("code")
	if issuedCode == "" {
		t.Fatal("want an authorization code to be issued")
	}

	// 3) /token：用 client 憑證換 access_token
	form := url.Values{
		"code":          {issuedCode},
		"client_id":     {"test-client"},
		"client_secret": {"test-secret"},
	}
	tokResp, err := http.Post(broker.URL+"/token", "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("token request failed: %v", err)
	}
	defer tokResp.Body.Close()
	if tokResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(tokResp.Body)
		t.Fatalf("want 200 from /token, got %d: %s", tokResp.StatusCode, body)
	}
	var tokBody struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
	}
	if err := json.NewDecoder(tokResp.Body).Decode(&tokBody); err != nil {
		t.Fatalf("failed to decode token response: %v", err)
	}
	if tokBody.AccessToken != "discord-token-1" {
		t.Fatalf("want access_token=discord-token-1, got %q", tokBody.AccessToken)
	}
	if tokBody.TokenType != "Bearer" {
		t.Fatalf("want token_type=Bearer, got %q", tokBody.TokenType)
	}

	// 同一組 code 不能重複兌換
	replayResp, err := http.Post(broker.URL+"/token", "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("replay token request failed: %v", err)
	}
	if replayResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want replaying a used code to fail with 400, got %d", replayResp.StatusCode)
	}

	// 4) /userinfo：驗證 sub/username/email/groups/picture
	req, _ := http.NewRequest(http.MethodGet, broker.URL+"/userinfo", nil)
	req.Header.Set("Authorization", "Bearer "+tokBody.AccessToken)
	uiResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("userinfo request failed: %v", err)
	}
	defer uiResp.Body.Close()
	if uiResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(uiResp.Body)
		t.Fatalf("want 200 from /userinfo, got %d: %s", uiResp.StatusCode, body)
	}
	var uiBody struct {
		Sub      string   `json:"sub"`
		Username string   `json:"username"`
		Email    string   `json:"email"`
		Groups   []string `json:"groups"`
		Picture  string   `json:"picture"`
	}
	if err := json.NewDecoder(uiResp.Body).Decode(&uiBody); err != nil {
		t.Fatalf("failed to decode userinfo response: %v", err)
	}
	if uiBody.Sub != "user-1" || uiBody.Username != "alice" || uiBody.Email != "alice@example.com" {
		t.Fatalf("unexpected userinfo: %+v", uiBody)
	}
	if len(uiBody.Groups) != 1 || uiBody.Groups[0] != "admin" {
		t.Fatalf("want groups=[admin], got %v", uiBody.Groups)
	}
	wantPicture := "https://cdn.discordapp.com/avatars/user-1/abc123.png"
	if uiBody.Picture != wantPicture {
		t.Fatalf("want picture=%q, got %q", wantPicture, uiBody.Picture)
	}
}

func TestHandleClientAuthorize_RejectsDisallowedRedirectURI(t *testing.T) {
	cfg := &Config{
		Guilds:              []GuildConfig{{ID: "guild-1"}},
		AllowedRedirectURIs: []string{"https://client.example.com/cb"},
	}
	broker := newTestBroker(newTestApp(cfg))
	defer broker.Close()

	resp, err := noRedirectClient().Get(broker.URL + "/authorize?redirect_uri=" + url.QueryEscape("https://evil.example.com") + "&state=x")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400 for a redirect_uri not in the allowlist, got %d", resp.StatusCode)
	}
}

func TestHandleClientAuthorize_MissingRedirectURI(t *testing.T) {
	cfg := &Config{Guilds: []GuildConfig{{ID: "guild-1"}}}
	broker := newTestBroker(newTestApp(cfg))
	defer broker.Close()

	resp, err := noRedirectClient().Get(broker.URL + "/authorize?state=x")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400 when redirect_uri is missing, got %d", resp.StatusCode)
	}
}

func TestHandleDiscordCallback_DeniedWhenGuildRulesNotMet(t *testing.T) {
	withMockDiscord(t, mockDiscordConfig{
		AccessToken: "discord-token-1",
		User:        map[string]interface{}{"id": "user-1", "username": "bob"},
		Guilds:      []map[string]interface{}{{"id": "some-other-guild"}},
	})

	cfg := &Config{
		Guilds:  []GuildConfig{{ID: "guild-1"}},
		Clients: []ClientConfig{{ID: "test-client", Secret: "test-secret"}},
	}
	app := newTestApp(cfg)
	broker := newTestBroker(app)
	defer broker.Close()

	client := noRedirectClient()
	authResp, err := client.Get(broker.URL + "/authorize?redirect_uri=" + url.QueryEscape("https://client.example.com/cb") + "&state=s1&client_id=test-client")
	if err != nil {
		t.Fatalf("authorize request failed: %v", err)
	}
	loc, _ := authResp.Location()
	sessionState := loc.Query().Get("state")

	cbResp, err := client.Get(broker.URL + "/callback?code=discord-code&state=" + sessionState)
	if err != nil {
		t.Fatalf("callback request failed: %v", err)
	}
	if cbResp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(cbResp.Body)
		t.Fatalf("want 403 when the user doesn't satisfy any guild rule, got %d: %s", cbResp.StatusCode, body)
	}
}

func TestHandleDiscordCallback_InvalidSession(t *testing.T) {
	cfg := &Config{Guilds: []GuildConfig{{ID: "guild-1"}}}
	broker := newTestBroker(newTestApp(cfg))
	defer broker.Close()

	resp, err := noRedirectClient().Get(broker.URL + "/callback?code=abc&state=does-not-exist")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400 for an unknown/expired session state, got %d", resp.StatusCode)
	}
}

func TestHandleClientToken_RequiresClientAuth(t *testing.T) {
	cfg := &Config{
		Guilds:  []GuildConfig{{ID: "guild-1"}},
		Clients: []ClientConfig{{ID: "test-client", Secret: "test-secret"}},
	}
	broker := newTestBroker(newTestApp(cfg))
	defer broker.Close()

	resp, err := http.Post(broker.URL+"/token", "application/x-www-form-urlencoded", strings.NewReader("code=whatever"))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401 when no client credentials are provided, got %d", resp.StatusCode)
	}
}

func TestHandleClientToken_RejectsCodeBoundToAnotherClient(t *testing.T) {
	withMockDiscord(t, mockDiscordConfig{
		AccessToken: "discord-token-1",
		User:        map[string]interface{}{"id": "user-1", "username": "carol"},
		Guilds:      []map[string]interface{}{{"id": "guild-1"}},
	})

	cfg := &Config{
		Guilds: []GuildConfig{{ID: "guild-1"}},
		Clients: []ClientConfig{
			{ID: "client-a", Secret: "secret-a"},
			{ID: "client-b", Secret: "secret-b"},
		},
	}
	broker := newTestBroker(newTestApp(cfg))
	defer broker.Close()

	client := noRedirectClient()
	authResp, err := client.Get(broker.URL + "/authorize?redirect_uri=" + url.QueryEscape("https://client.example.com/cb") + "&state=s1&client_id=client-a")
	if err != nil {
		t.Fatalf("authorize request failed: %v", err)
	}
	loc, _ := authResp.Location()
	sessionState := loc.Query().Get("state")

	cbResp, err := client.Get(broker.URL + "/callback?code=discord-code&state=" + sessionState)
	if err != nil {
		t.Fatalf("callback request failed: %v", err)
	}
	clientLoc, _ := cbResp.Location()
	issuedCode := clientLoc.Query().Get("code")

	// client-b（憑證正確）想拿本來發給 client-a 的 code 換 token，應該被拒絕
	form := url.Values{"code": {issuedCode}, "client_id": {"client-b"}, "client_secret": {"secret-b"}}
	tokResp, err := http.Post(broker.URL+"/token", "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("token request failed: %v", err)
	}
	if tokResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400 when the code is redeemed by a different client than it was issued to, got %d", tokResp.StatusCode)
	}
}

func TestHandleClientUserInfo_MissingToken(t *testing.T) {
	cfg := &Config{Guilds: []GuildConfig{{ID: "guild-1"}}}
	broker := newTestBroker(newTestApp(cfg))
	defer broker.Close()

	resp, err := http.Get(broker.URL + "/userinfo")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401 when no Authorization header is set, got %d", resp.StatusCode)
	}
}

func TestHandleHealthz(t *testing.T) {
	broker := newTestBroker(newTestApp(&Config{Guilds: []GuildConfig{{ID: "guild-1"}}}))
	defer broker.Close()

	resp, err := http.Get(broker.URL + "/healthz")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("want status=ok, got %v", body)
	}
}
