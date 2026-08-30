package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/joho/godotenv"
)

// loadEnv 讀取本服務向 Discord 註冊的 OAuth client 機敏資訊。
// 這些屬於機敏設定，放在 .env（不進版控），不同於 config.yaml 的白名單設定。
func loadEnv() (clientID, clientSecret, redirectURI string) {
	_ = godotenv.Load()

	clientID = os.Getenv("DISCORD_CLIENT_ID")
	clientSecret = os.Getenv("DISCORD_CLIENT_SECRET")
	redirectURI = os.Getenv("DISCORD_REDIRECT_URI")

	if clientID == "" || clientSecret == "" || redirectURI == "" {
		log.Fatal("Environment variables are missing. Please check configuration.")
	}
	return
}

func main() {
	clientID, clientSecret, redirectURI := loadEnv()

	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "config.yaml"
	}
	cfg, err := LoadConfig(configPath)
	if err != nil {
		log.Fatalf("載入設定檔失敗: %v", err)
	}
	if len(cfg.Clients) == 0 {
		log.Println("[Warn] 未設定 clients，/authorize 與 /token 端點將拒絕所有請求")
	}
	for _, c := range cfg.Clients {
		if len(c.RedirectURIs) == 0 {
			log.Printf("[Warn] client_id=%s 未設定 redirectURIs，將接受任意 redirect_uri（不建議用於正式環境）", c.ID)
		}
	}

	app := &App{
		clientID:     clientID,
		clientSecret: clientSecret,
		redirectURI:  redirectURI,
		config:       cfg,
		store:        NewStore(time.Minute),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", handleHealthz)
	mux.HandleFunc("/authorize", app.handleClientAuthorize)
	mux.HandleFunc("/token", app.handleClientToken)
	mux.HandleFunc("/userinfo", app.handleClientUserInfo)
	mux.HandleFunc("/callback", app.handleDiscordCallback)

	log.Println("🚀 Discord OAuth Broker 啟動於 http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}
