package broker

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	pendingSessionTTL = 10 * time.Minute // 使用者完成 Discord 登入的時間上限
	issuedCodeTTL     = 2 * time.Minute  // client 拿 code 換 token 的時間上限
	accessTokenTTL    = 1 * time.Hour    // broker 核發的 access_token 有效期限
)

// handleHealthz 供 docker-compose healthcheck / 監控探活使用。
func handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// 處理來自下游服務的授權請求
func (a *App) handleClientAuthorize(w http.ResponseWriter, r *http.Request) {
	clientRedirectURI := r.URL.Query().Get("redirect_uri")
	state := r.URL.Query().Get("state")
	clientID := r.URL.Query().Get("client_id")

	// client_id 必填，且需要是 config.yaml 裡登記過的 client；
	// redirect_uri 只會跟這個 client 自己的白名單比對，跟其他 client 互相隔離，
	// 避免 A client 借用 B client 登記的 redirect_uri 發起登入流程。
	if clientID == "" {
		http.Error(w, "缺少 client_id", http.StatusBadRequest)
		return
	}
	client, ok := a.config.FindClient(clientID)
	if !ok {
		log.Printf("[Warn] 未知的 client_id: %s", clientID)
		http.Error(w, "未知的 client_id", http.StatusBadRequest)
		return
	}
	if clientRedirectURI == "" {
		http.Error(w, "缺少 redirect_uri", http.StatusBadRequest)
		return
	}
	if !client.IsRedirectURIAllowed(clientRedirectURI) {
		log.Printf("[Warn] client_id=%s 使用未登記的 redirect_uri: %s", clientID, clientRedirectURI)
		http.Error(w, "redirect_uri 不被允許", http.StatusBadRequest)
		return
	}

	sessionID, err := generateRandomID("sess_")
	if err != nil {
		log.Printf("[Error] 產生 sessionID 失敗: %v", err)
		http.Error(w, "內部錯誤", http.StatusInternalServerError)
		return
	}

	a.store.Set(sessionID, map[string]string{
		"client_redirect": clientRedirectURI,
		"client_state":    state,
		"client_id":       clientID,
	}, pendingSessionTTL)

	q := url.Values{}
	q.Add("client_id", a.clientID)
	q.Add("redirect_uri", a.redirectURI)
	q.Add("response_type", "code")
	// guilds.members.read 用於查詢使用者在伺服器內的角色，供 roleIds 白名單比對
	q.Add("scope", "identify email guilds guilds.members.read")
	q.Add("state", sessionID)

	discordAuthURL := "https://discord.com/oauth2/authorize?" + q.Encode()
	http.Redirect(w, r, discordAuthURL, http.StatusFound)
}

// 接收 Discord 回傳的 Code
func (a *App) handleDiscordCallback(w http.ResponseWriter, r *http.Request) {
	discordCode := r.URL.Query().Get("code")
	sessionID := r.URL.Query().Get("state")

	if discordCode == "" {
		http.Error(w, "沒有拿到 Discord Code", http.StatusBadRequest)
		return
	}

	clientData, ok := a.store.Pop(sessionID)
	if !ok {
		http.Error(w, "Session 過期或無效，請重新登入", http.StatusBadRequest)
		return
	}

	discordToken, err := exchangeToken(a.clientID, a.clientSecret, a.redirectURI, discordCode)
	if err != nil {
		http.Error(w, "換 Token 失敗", http.StatusInternalServerError)
		return
	}

	// 順便取得使用者身份，供稽核 log 使用（誰、何時、通過與否）
	discordUser, err := fetchDiscordUser(discordToken)
	if err != nil {
		log.Printf("[Error] 無法取得使用者資訊: %v", err)
		http.Error(w, "無法取得使用者資訊", http.StatusInternalServerError)
		return
	}
	userID, _ := discordUser["id"].(string)
	username, _ := discordUser["username"].(string)

	allowed, groups, err := checkGuildAccess(discordToken, a.config)
	if err != nil {
		log.Printf("[Audit] login user_id=%s username=%s result=error reason=%q ip=%s", userID, username, err, r.RemoteAddr)
		http.Error(w, "無法取得使用者伺服器資訊", http.StatusInternalServerError)
		return
	}
	if !allowed {
		log.Printf("[Audit] login user_id=%s username=%s result=denied ip=%s", userID, username, r.RemoteAddr)
		http.Error(w, "抱歉，你不符合登入資格（伺服器或角色不符）", http.StatusForbidden)
		return
	}
	log.Printf("[Audit] login user_id=%s username=%s result=allowed groups=%v ip=%s", userID, username, groups, r.RemoteAddr)

	myCode, err := generateRandomID("code_")
	if err != nil {
		log.Printf("[Error] 產生授權碼失敗: %v", err)
		http.Error(w, "內部錯誤", http.StatusInternalServerError)
		return
	}

	a.store.Set(myCode, map[string]string{
		"discord_token": discordToken,
		"client_id":     clientData["client_id"],
	}, issuedCodeTTL)

	finalRedirect := fmt.Sprintf("%s?code=%s&state=%s",
		clientData["client_redirect"],
		myCode,
		clientData["client_state"],
	)

	http.Redirect(w, r, finalRedirect, http.StatusFound)
}

// 客戶端拿 Code 來換 Token
func (a *App) handleClientToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "無法解析請求內容", http.StatusBadRequest)
		return
	}

	clientID, ok := authenticateClient(r, a.config)
	if !ok {
		log.Println("[Error] /token 呼叫者身份驗證失敗")
		http.Error(w, "invalid_client", http.StatusUnauthorized)
		return
	}

	code := r.FormValue("code")
	userData, ok := a.store.Pop(code)
	if !ok {
		http.Error(w, "Invalid Code", http.StatusBadRequest)
		log.Println("[Error] Invalid Code provided")
		return
	}

	// 授權碼在 /authorize 階段記錄的 client_id，必須與這裡通過驗證的 client_id
	// 一致，避免授權碼被其他 client 冒用。
	if boundClientID := userData["client_id"]; boundClientID != clientID {
		log.Printf("[Error] code 綁定的 client_id (%s) 與請求 client_id (%s) 不符", boundClientID, clientID)
		http.Error(w, "Invalid Code", http.StatusBadRequest)
		return
	}

	// 核發本服務自己的 opaque access_token，不把使用者的原始 Discord token
	// 直接交給下游服務：
	//   1. 就算這個 access_token 外流，拿到的人也打不了 Discord API，
	//      對 Discord 來說它毫無意義；
	//   2. /userinfo 才能靠這裡存的紀錄驗證「這是不是本服務自己發出去的
	//      token」，避免任何人拿隨便一個 Discord token（跟本服務的登入流程
	//      毫無關係）就能查到使用者的 groups/角色等敏感資訊。
	accessToken, err := generateRandomID("at_")
	if err != nil {
		log.Printf("[Error] 產生 access token 失敗: %v", err)
		http.Error(w, "內部錯誤", http.StatusInternalServerError)
		return
	}
	a.store.Set(accessToken, map[string]string{
		"discord_token": userData["discord_token"],
		"client_id":     clientID,
	}, accessTokenTTL)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"access_token": accessToken,
		"token_type":   "Bearer",
		"expires_in":   int(accessTokenTTL.Seconds()),
	})
}

// 客戶端拿 Token 來要使用者資料
func (a *App) handleClientUserInfo(w http.ResponseWriter, r *http.Request) {
	const bearerPrefix = "Bearer "
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, bearerPrefix) {
		http.Error(w, "Missing Token", http.StatusUnauthorized)
		return
	}
	accessToken := strings.TrimPrefix(authHeader, bearerPrefix)

	// 必須是本服務自己核發過的 access_token 才放行，拒絕任何來路不明、
	// 未經過 /authorize -> /callback -> /token 這條流程的 Discord token。
	tokenData, ok := a.store.Get(accessToken)
	if !ok {
		http.Error(w, "Invalid Token", http.StatusUnauthorized)
		return
	}
	discordToken := tokenData["discord_token"]

	discordUser, err := fetchDiscordUser(discordToken)
	if err != nil {
		http.Error(w, "無法取得 Discord 使用者資訊", http.StatusInternalServerError)
		return
	}

	username, _ := discordUser["username"].(string)
	email, _ := discordUser["email"].(string)
	id, _ := discordUser["id"].(string)

	// 重新比對一次伺服器/角色規則，取得目前的 groups claim；
	// 現算現查而不是沿用登入當下的結果，這樣角色異動後不用重新登入就會反映。
	_, groups, err := checkGuildAccess(discordToken, a.config)
	if err != nil {
		log.Printf("[Warn] 無法計算 groups claim: %v", err)
	}

	resp := map[string]interface{}{
		"sub":      id,
		"username": username,
		"name":     username,
		"email":    email,
		"groups":   groups,
	}
	if picture := discordAvatarURL(discordUser); picture != "" {
		resp["picture"] = picture
	}

	w.Header().Set("Content-Type", "application/json")
	// 「大包裝」回傳，同時滿足多種服務需求
	json.NewEncoder(w).Encode(resp)
}

// discordAvatarURL 組出使用者頭像的 CDN URL；沒有自訂頭像時回傳空字串。
func discordAvatarURL(discordUser map[string]interface{}) string {
	id, _ := discordUser["id"].(string)
	avatar, _ := discordUser["avatar"].(string)
	if id == "" || avatar == "" {
		return ""
	}
	ext := "png"
	if strings.HasPrefix(avatar, "a_") {
		ext = "gif" // 動態頭像
	}
	return fmt.Sprintf("https://cdn.discordapp.com/avatars/%s/%s.%s", id, avatar, ext)
}
