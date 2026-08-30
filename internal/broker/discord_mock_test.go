package broker

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// mockDiscordConfig 描述假 Discord server 要回什麼資料/狀態碼。
// 零值代表「用預設值」（狀態碼預設 200，accessToken 預設 "mock-access-token"）。
type mockDiscordConfig struct {
	AccessToken  string
	TokenStatus  int
	User         map[string]interface{}
	UserStatus   int
	Guilds       []map[string]interface{}
	GuildsStatus int
	// RolesByGuild：guild id -> 角色清單。查詢不在這個 map 裡的 guild 會回 403，
	// 模擬「這個 guild 沒有開放 guilds.members.read 或使用者不在其中」的情況。
	RolesByGuild map[string][]string
}

// newMockDiscord 啟動一個假的 Discord API server，實作測試會用到的四個端點：
// POST /oauth2/token、GET /users/@me、GET /users/@me/guilds、
// GET /users/@me/guilds/{id}/member。呼叫端記得 defer Close()。
func newMockDiscord(t *testing.T, cfg mockDiscordConfig) *httptest.Server {
	t.Helper()

	if cfg.AccessToken == "" {
		cfg.AccessToken = "mock-access-token"
	}
	if cfg.TokenStatus == 0 {
		cfg.TokenStatus = http.StatusOK
	}
	if cfg.UserStatus == 0 {
		cfg.UserStatus = http.StatusOK
	}
	if cfg.GuildsStatus == 0 {
		cfg.GuildsStatus = http.StatusOK
	}

	requireBearer := func(w http.ResponseWriter, r *http.Request) bool {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer "+cfg.AccessToken {
			w.WriteHeader(http.StatusUnauthorized)
			return false
		}
		return true
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		if cfg.TokenStatus != http.StatusOK {
			w.WriteHeader(cfg.TokenStatus)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": cfg.AccessToken})
	})

	mux.HandleFunc("/users/@me", func(w http.ResponseWriter, r *http.Request) {
		if !requireBearer(w, r) {
			return
		}
		if cfg.UserStatus != http.StatusOK {
			w.WriteHeader(cfg.UserStatus)
			return
		}
		_ = json.NewEncoder(w).Encode(cfg.User)
	})

	mux.HandleFunc("/users/@me/guilds", func(w http.ResponseWriter, r *http.Request) {
		if !requireBearer(w, r) {
			return
		}
		if cfg.GuildsStatus != http.StatusOK {
			w.WriteHeader(cfg.GuildsStatus)
			return
		}
		_ = json.NewEncoder(w).Encode(cfg.Guilds)
	})

	// /users/@me/guilds/{id}/member
	mux.HandleFunc("/users/@me/guilds/", func(w http.ResponseWriter, r *http.Request) {
		if !requireBearer(w, r) {
			return
		}
		guildID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/users/@me/guilds/"), "/member")
		roles, ok := cfg.RolesByGuild[guildID]
		if !ok {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"roles": roles})
	})

	return httptest.NewServer(mux)
}

// withMockDiscord 把 discordAPIBase 換成假 server 的 URL，測試結束後自動還原，
// 避免測試之間互相污染全域狀態。
func withMockDiscord(t *testing.T, cfg mockDiscordConfig) *httptest.Server {
	t.Helper()
	server := newMockDiscord(t, cfg)
	original := discordAPIBase
	discordAPIBase = server.URL
	t.Cleanup(func() {
		discordAPIBase = original
		server.Close()
	})
	return server
}
