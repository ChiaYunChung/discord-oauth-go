package main

import "log"

// checkGuildAccess 依序檢查 config.yaml 中的每個 guild：
//  1. 使用者必須是該 guild 成員
//  2. 若該 guild 有設定 roleIds，使用者必須擁有其中至少一個角色
//
// 只要符合其中一組 guild 規則即視為通過（allowed=true）。
// 會掃過所有規則（而非命中第一筆就停），把每筆命中規則的 GroupName 收集成
// groups 回傳，供 /userinfo 當作 groups claim 使用；同一個 guild 若被查詢
// 多次角色，只會呼叫一次 Discord API（同次呼叫內快取)。
func checkGuildAccess(token string, cfg *Config) (allowed bool, groups []string, err error) {
	guilds, err := fetchUserGuilds(token)
	if err != nil {
		return false, nil, err
	}

	memberOf := make(map[string]bool, len(guilds))
	for _, g := range guilds {
		if id, ok := g["id"].(string); ok {
			memberOf[id] = true
		}
	}

	roleCache := make(map[string][]string)
	seenGroup := make(map[string]bool)

	for _, gc := range cfg.Guilds {
		if !memberOf[gc.ID] {
			continue
		}

		matched := len(gc.RoleIDs) == 0
		if !matched {
			roles, ok := roleCache[gc.ID]
			if !ok {
				roles, err = fetchGuildMemberRoles(token, gc.ID)
				if err != nil {
					// 單一 guild 查角色失敗不應直接判定失敗，改試下一組規則，
					// 但要留下記錄方便排查。
					log.Printf("[Warn] 無法取得 guild %s 的角色資訊: %v", gc.ID, err)
					continue
				}
				roleCache[gc.ID] = roles
			}
			matched = hasAnyRole(roles, gc.RoleIDs)
		}

		if matched {
			allowed = true
			if gc.GroupName != "" && !seenGroup[gc.GroupName] {
				seenGroup[gc.GroupName] = true
				groups = append(groups, gc.GroupName)
			}
		}
	}

	return allowed, groups, nil
}

// hasAnyRole 回傳 userRoles 是否包含 required 其中至少一個角色。
func hasAnyRole(userRoles, required []string) bool {
	set := make(map[string]bool, len(userRoles))
	for _, r := range userRoles {
		set[r] = true
	}
	for _, req := range required {
		if set[req] {
			return true
		}
	}
	return false
}
