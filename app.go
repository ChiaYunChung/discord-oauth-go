package main

// App 集中管理服務所需的設定與狀態，取代原本的全域變數，
// 方便之後測試與擴充。
type App struct {
	clientID     string // 本服務向 Discord 註冊的 OAuth client id
	clientSecret string
	redirectURI  string
	config       *Config
	store        *Store
}
