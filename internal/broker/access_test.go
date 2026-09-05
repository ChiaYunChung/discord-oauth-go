package broker

import (
	"reflect"
	"sort"
	"testing"
)

func TestCheckGuildAccess_MembershipOnlyNoRolesRequired(t *testing.T) {
	withMockDiscord(t, mockDiscordConfig{
		AccessToken: "tok",
		Guilds:      []map[string]interface{}{{"id": "guild-1"}},
	})

	cfg := &Config{Guilds: []GuildConfig{{ID: "guild-1"}}} // roleIds 留空

	allowed, groups, err := checkGuildAccess("tok", cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Fatal("want allowed=true for plain membership")
	}
	if len(groups) != 0 {
		t.Fatalf("want no groups when groupName unset, got %v", groups)
	}
}

func TestCheckGuildAccess_DeniedWhenNotMember(t *testing.T) {
	withMockDiscord(t, mockDiscordConfig{
		AccessToken: "tok",
		Guilds:      []map[string]interface{}{{"id": "other-guild"}},
	})

	cfg := &Config{Guilds: []GuildConfig{{ID: "guild-1"}}}

	allowed, _, err := checkGuildAccess("tok", cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allowed {
		t.Fatal("want allowed=false when user is not a member of any configured guild")
	}
}

func TestCheckGuildAccess_RoleRequired(t *testing.T) {
	withMockDiscord(t, mockDiscordConfig{
		AccessToken:  "tok",
		Guilds:       []map[string]interface{}{{"id": "guild-1"}},
		RolesByGuild: map[string][]string{"guild-1": {"role-x"}},
	})

	t.Run("has one of the required roles -> allowed", func(t *testing.T) {
		cfg := &Config{Guilds: []GuildConfig{
			{ID: "guild-1", RoleIDs: []string{"role-x", "role-y"}, GroupName: "admin"},
		}}
		allowed, groups, err := checkGuildAccess("tok", cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !allowed {
			t.Fatal("want allowed=true when user has one of the required roles")
		}
		if !reflect.DeepEqual(groups, []string{"admin"}) {
			t.Fatalf("want groups=[admin], got %v", groups)
		}
	})

	t.Run("has none of the required roles -> denied", func(t *testing.T) {
		cfg := &Config{Guilds: []GuildConfig{
			{ID: "guild-1", RoleIDs: []string{"role-not-held"}},
		}}
		allowed, _, err := checkGuildAccess("tok", cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if allowed {
			t.Fatal("want allowed=false when user has none of the required roles")
		}
	})
}

func TestCheckGuildAccess_CollectsGroupsFromMultipleMatchingRules(t *testing.T) {
	withMockDiscord(t, mockDiscordConfig{
		AccessToken:  "tok",
		Guilds:       []map[string]interface{}{{"id": "guild-1"}, {"id": "guild-2"}},
		RolesByGuild: map[string][]string{"guild-1": {"role-admin"}},
	})

	// guild-1 拆成 admin / member 兩筆規則，同時符合兩者；guild-2 只要求成員身份。
	cfg := &Config{Guilds: []GuildConfig{
		{ID: "guild-1", RoleIDs: []string{"role-admin"}, GroupName: "admin"},
		{ID: "guild-1", GroupName: "member"},
		{ID: "guild-2", GroupName: "another-server-member"},
	}}

	allowed, groups, err := checkGuildAccess("tok", cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Fatal("want allowed=true")
	}

	sort.Strings(groups)
	want := []string{"admin", "another-server-member", "member"}
	if !reflect.DeepEqual(groups, want) {
		t.Fatalf("want groups=%v, got %v", want, groups)
	}
}

func TestCheckGuildAccess_GuildsAPIError(t *testing.T) {
	withMockDiscord(t, mockDiscordConfig{AccessToken: "tok", GuildsStatus: 500})

	cfg := &Config{Guilds: []GuildConfig{{ID: "guild-1"}}}

	if _, _, err := checkGuildAccess("tok", cfg); err == nil {
		t.Fatal("want error when /users/@me/guilds fails, got nil")
	}
}

func TestCheckGuildAccess_RoleLookupErrorSkipsRuleButKeepsChecking(t *testing.T) {
	// guild-1 的角色查詢會失敗（mock 沒設定 RolesByGuild["guild-1"]），
	// 但 guild-2 只要求成員身份，應該仍然通過。
	withMockDiscord(t, mockDiscordConfig{
		AccessToken: "tok",
		Guilds:      []map[string]interface{}{{"id": "guild-1"}, {"id": "guild-2"}},
	})

	cfg := &Config{Guilds: []GuildConfig{
		{ID: "guild-1", RoleIDs: []string{"role-x"}},
		{ID: "guild-2"},
	}}

	allowed, _, err := checkGuildAccess("tok", cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Fatal("want allowed=true via guild-2 even though guild-1 role lookup failed")
	}
}

func TestHasAnyRole(t *testing.T) {
	cases := []struct {
		name      string
		userRoles []string
		required  []string
		want      bool
	}{
		{"empty required matches nothing needed but returns false", []string{"a"}, nil, false},
		{"one overlapping role", []string{"a", "b"}, []string{"b", "c"}, true},
		{"no overlap", []string{"a"}, []string{"b"}, false},
		{"empty user roles", nil, []string{"a"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasAnyRole(tc.userRoles, tc.required); got != tc.want {
				t.Fatalf("hasAnyRole(%v, %v) = %v, want %v", tc.userRoles, tc.required, got, tc.want)
			}
		})
	}
}
