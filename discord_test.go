package main

import (
	"net/http"
	"testing"
)

func TestExchangeToken_Success(t *testing.T) {
	withMockDiscord(t, mockDiscordConfig{AccessToken: "tok-abc"})

	token, err := exchangeToken("cid", "csecret", "https://broker/callback", "code-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "tok-abc" {
		t.Fatalf("want tok-abc, got %q", token)
	}
}

func TestExchangeToken_DiscordError(t *testing.T) {
	withMockDiscord(t, mockDiscordConfig{TokenStatus: http.StatusBadRequest})

	if _, err := exchangeToken("cid", "csecret", "https://broker/callback", "bad-code"); err == nil {
		t.Fatal("want error when discord returns non-200, got nil")
	}
}

func TestFetchDiscordUser(t *testing.T) {
	withMockDiscord(t, mockDiscordConfig{
		AccessToken: "tok-1",
		User:        map[string]interface{}{"id": "u1", "username": "alice"},
	})

	user, err := fetchDiscordUser("tok-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user["id"] != "u1" || user["username"] != "alice" {
		t.Fatalf("unexpected user: %v", user)
	}
}

func TestFetchDiscordUser_Unauthorized(t *testing.T) {
	withMockDiscord(t, mockDiscordConfig{AccessToken: "tok-1"})

	if _, err := fetchDiscordUser("wrong-token"); err == nil {
		t.Fatal("want error for wrong token, got nil")
	}
}

func TestFetchUserGuilds(t *testing.T) {
	withMockDiscord(t, mockDiscordConfig{
		AccessToken: "tok-1",
		Guilds: []map[string]interface{}{
			{"id": "g1"}, {"id": "g2"},
		},
	})

	guilds, err := fetchUserGuilds("tok-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(guilds) != 2 {
		t.Fatalf("want 2 guilds, got %d", len(guilds))
	}
}

func TestFetchGuildMemberRoles(t *testing.T) {
	withMockDiscord(t, mockDiscordConfig{
		AccessToken:  "tok-1",
		RolesByGuild: map[string][]string{"g1": {"role-a", "role-b"}},
	})

	roles, err := fetchGuildMemberRoles("tok-1", "g1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(roles) != 2 || roles[0] != "role-a" {
		t.Fatalf("unexpected roles: %v", roles)
	}
}

func TestFetchGuildMemberRoles_NotAMember(t *testing.T) {
	withMockDiscord(t, mockDiscordConfig{
		AccessToken:  "tok-1",
		RolesByGuild: map[string][]string{"g1": {"role-a"}},
	})

	if _, err := fetchGuildMemberRoles("tok-1", "g-not-configured"); err == nil {
		t.Fatal("want error for guild the mock doesn't know about, got nil")
	}
}
