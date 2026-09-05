package broker

import "net/http"

// App 集中管理服務所需的設定與狀態，取代原本的全域變數，
// 方便之後測試與擴充。
type App struct {
	clientID     string // 本服務向 Discord 註冊的 OAuth client id
	clientSecret string
	redirectURI  string
	config       *Config
	store        *Store
}

// NewApp 建立一個設定好的 App。
// clientID/clientSecret/redirectURI 是本服務向 Discord 註冊的 OAuth App 憑證，
// cfg 是 config.yaml 讀進來的內容，store 是 session/授權碼的暫存區。
func NewApp(clientID, clientSecret, redirectURI string, cfg *Config, store *Store) *App {
	return &App{
		clientID:     clientID,
		clientSecret: clientSecret,
		redirectURI:  redirectURI,
		config:       cfg,
		store:        store,
	}
}

// Routes 組出這個服務對外提供的所有 HTTP 路由。
func (a *App) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", handleHealthz)
	mux.HandleFunc("/authorize", a.handleClientAuthorize)
	mux.HandleFunc("/token", a.handleClientToken)
	mux.HandleFunc("/userinfo", a.handleClientUserInfo)
	mux.HandleFunc("/callback", a.handleDiscordCallback)
	return mux
}
