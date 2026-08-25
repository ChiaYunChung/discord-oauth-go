package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/joho/godotenv"
)

var (
	clientID     string
	clientSecret string
	redirectURI  string
	labGuildID   string
	// 記錄客戶端登入請求的資訊
	sessionStore = make(map[string]map[string]string)
	storeMutex   sync.RWMutex
)

func loadEnv() {
	_ = godotenv.Load()

	clientID = os.Getenv("DISCORD_CLIENT_ID")
	clientSecret = os.Getenv("DISCORD_CLIENT_SECRET")
	redirectURI = os.Getenv("DISCORD_REDIRECT_URI")
	labGuildID = os.Getenv("LAB_GUILD_ID")

	if clientID == "" || clientSecret == "" || redirectURI == "" || labGuildID == "" {
		log.Fatal("Environment variables are missing. Please check configuration.")
	}
}

// 處理來自 Portainer 或 LiteLLM 的授權請求
func handleClientAuthorize(w http.ResponseWriter, r *http.Request) {
	clientRedirectURI := r.URL.Query().Get("redirect_uri")
	state := r.URL.Query().Get("state")

	sessionID := fmt.Sprintf("sess_%d", time.Now().UnixNano())

	storeMutex.Lock()
	sessionStore[sessionID] = map[string]string{
		"client_redirect": clientRedirectURI,
		"client_state":    state,
	}
	storeMutex.Unlock()

	q := url.Values{}
	q.Add("client_id", clientID)
	q.Add("redirect_uri", redirectURI)
	q.Add("response_type", "code")
	// 加上 email 權限供 LiteLLM 使用
	q.Add("scope", "identify email guilds")
	q.Add("state", sessionID)

	discordAuthURL := "https://discord.com/oauth2/authorize?" + q.Encode()
	http.Redirect(w, r, discordAuthURL, http.StatusFound)
}

// 接收 Discord 回傳的 Code
func handleDiscordCallback(w http.ResponseWriter, r *http.Request) {
	discordCode := r.URL.Query().Get("code")
	sessionID := r.URL.Query().Get("state")

	if discordCode == "" {
		http.Error(w, "沒有拿到 Discord Code", http.StatusBadRequest)
		return
	}

	discordToken, err := exchangeToken(discordCode)
	if err != nil {
		http.Error(w, "換 Token 失敗", http.StatusInternalServerError)
		return
	}

	guilds, err := fetchUserGuilds(discordToken)
	if err != nil {
		http.Error(w, "無法取得使用者伺服器清單", http.StatusInternalServerError)
		return
	}

	isLabMember := false
	for _, g := range guilds {
		if g["id"] == labGuildID {
			isLabMember = true
			break
		}
	}

	if !isLabMember {
		http.Error(w, "抱歉，你不是實驗室 Discord 成員，無法登入！", http.StatusForbidden)
		return
	}

	storeMutex.RLock()
	clientData, ok := sessionStore[sessionID]
	storeMutex.RUnlock()

	if !ok {
		http.Error(w, "Session 過期或無效，請重新登入", http.StatusBadRequest)
		return
	}

	myCode := fmt.Sprintf("code_%d", time.Now().UnixNano())

	storeMutex.Lock()
	delete(sessionStore, sessionID)
	sessionStore[myCode] = map[string]string{
		"discord_token": discordToken,
	}
	storeMutex.Unlock()

	finalRedirect := fmt.Sprintf("%s?code=%s&state=%s",
		clientData["client_redirect"],
		myCode,
		clientData["client_state"],
	)

	http.Redirect(w, r, finalRedirect, http.StatusFound)
}

// 客戶端拿 Code 來換 Token
func handleClientToken(w http.ResponseWriter, r *http.Request) {
	fmt.Println("[Debug] Client 拿著 Code 來換 Token 了！")
	r.ParseForm()
	code := r.FormValue("code")

	storeMutex.Lock()
	userData, ok := sessionStore[code]
	if ok {
		delete(sessionStore, code)
	}
	storeMutex.Unlock()

	if !ok {
		http.Error(w, "Invalid Code", http.StatusBadRequest)
		log.Println("[Error] Invalid Code provided")
		return
	}

	discordToken := userData["discord_token"]

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"access_token": "%s", "token_type": "Bearer"}`, discordToken)
}

// 客戶端拿 Token 來要使用者資料
func handleClientUserInfo(w http.ResponseWriter, r *http.Request) {
	fmt.Println("[Debug] Client 拿 Token 來問資訊了！")
	authHeader := r.Header.Get("Authorization")
	if len(authHeader) < 7 {
		http.Error(w, "Missing Token", http.StatusUnauthorized)
		return
	}
	discordToken := authHeader[7:]

	discordUser, err := fetchDiscordUser(discordToken)
	if err != nil {
		http.Error(w, "無法取得 Discord 使用者資訊", http.StatusInternalServerError)
		return
	}

	username, _ := discordUser["username"].(string)
	email, _ := discordUser["email"].(string)
	id, _ := discordUser["id"].(string)

	w.Header().Set("Content-Type", "application/json")
	// 「大包裝」回傳，同時滿足多種服務需求
	responseJSON := fmt.Sprintf(
		`{"sub": "%s", "username": "%s", "name": "%s", "email": "%s"}`,
		id, username, username, email,
	)
	fmt.Fprint(w, responseJSON)
}

func fetchUserGuilds(token string) ([]map[string]interface{}, error) {
	req, _ := http.NewRequest("GET", "https://discord.com/api/users/@me/guilds", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var guilds []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&guilds); err != nil {
		return nil, err
	}
	return guilds, nil
}

func exchangeToken(code string) (string, error) {
	log.Println("[Debug] 準備聯繫 Discord...")
	data := url.Values{}
	data.Set("client_id", clientID)
	data.Set("client_secret", clientSecret)
	data.Set("grant_type", "authorization_code")
	data.Set("code", code)
	data.Set("redirect_uri", redirectURI)

	req, _ := http.NewRequest("POST", "https://discord.com/api/oauth2/token", strings.NewReader(data.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("[Error] Discord 換 Token 失敗: %s", string(body))
		return "", fmt.Errorf("discord auth failed: %s", string(body))
	}

	var res struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", err
	}
	return res.AccessToken, nil
}

func fetchDiscordUser(token string) (map[string]interface{}, error) {
	req, _ := http.NewRequest("GET", "https://discord.com/api/users/@me", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var user map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, err
	}
	return user, nil
}

func main() {
	loadEnv()

	http.HandleFunc("/authorize", handleClientAuthorize)
	http.HandleFunc("/token", handleClientToken)
	http.HandleFunc("/userinfo", handleClientUserInfo)
	http.HandleFunc("/callback", handleDiscordCallback)

	log.Println("🚀 實驗室 SSO Broker 啟動於 http://localhost:8080")
	http.ListenAndServe(":8080", nil)
}