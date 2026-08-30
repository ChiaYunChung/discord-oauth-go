package broker

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
)

// discordAPIBase 是可替換的變數（而非 const），讓測試能把它指向假的 Discord
// API server，藉此測試整條 HTTP 流程而不必真的呼叫 Discord。
var discordAPIBase = "https://discord.com/api"

// exchangeToken 拿 Discord 回傳的 authorization code 換取 access token。
func exchangeToken(clientID, clientSecret, redirectURI, code string) (string, error) {
	data := url.Values{}
	data.Set("client_id", clientID)
	data.Set("client_secret", clientSecret)
	data.Set("grant_type", "authorization_code")
	data.Set("code", code)
	data.Set("redirect_uri", redirectURI)

	req, err := http.NewRequest("POST", discordAPIBase+"/oauth2/token", strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("[Error] Discord 換 Token 失敗 (status=%d): %s", resp.StatusCode, string(body))
		return "", fmt.Errorf("discord auth failed: status %d", resp.StatusCode)
	}

	var res struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", err
	}
	return res.AccessToken, nil
}

// discordAPIGet 呼叫需要 Bearer token 的 Discord GET API，並檢查回應狀態碼。
func discordAPIGet(token, path string, out interface{}) error {
	req, err := http.NewRequest("GET", discordAPIBase+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("discord api %s failed (status=%d): %s", path, resp.StatusCode, string(body))
	}

	return json.NewDecoder(resp.Body).Decode(out)
}

func fetchDiscordUser(token string) (map[string]interface{}, error) {
	var user map[string]interface{}
	if err := discordAPIGet(token, "/users/@me", &user); err != nil {
		return nil, err
	}
	return user, nil
}

func fetchUserGuilds(token string) ([]map[string]interface{}, error) {
	var guilds []map[string]interface{}
	if err := discordAPIGet(token, "/users/@me/guilds", &guilds); err != nil {
		return nil, err
	}
	return guilds, nil
}

// fetchGuildMemberRoles 取得使用者在指定 guild 內的角色清單。
// 需要 OAuth scope 包含 guilds.members.read。
func fetchGuildMemberRoles(token, guildID string) ([]string, error) {
	var member struct {
		Roles []string `json:"roles"`
	}
	if err := discordAPIGet(token, "/users/@me/guilds/"+guildID+"/member", &member); err != nil {
		return nil, err
	}
	return member.Roles, nil
}
