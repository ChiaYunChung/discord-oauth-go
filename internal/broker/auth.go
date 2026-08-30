package broker

import (
	"crypto/subtle"
	"net/http"
)

// authenticateClient 驗證呼叫 /token 的 client 身份，回傳通過驗證的 client_id。
// 依 RFC 6749，同時支援 HTTP Basic Authorization 及 form body 兩種帶法，
// 相容不同 OAuth client 實作。
func authenticateClient(r *http.Request, cfg *Config) (string, bool) {
	clientID, clientSecret, ok := r.BasicAuth()
	if !ok {
		clientID = r.FormValue("client_id")
		clientSecret = r.FormValue("client_secret")
	}
	if clientID == "" || clientSecret == "" {
		return "", false
	}

	client, found := cfg.FindClient(clientID)
	if !found {
		return "", false
	}
	if subtle.ConstantTimeCompare([]byte(client.Secret), []byte(clientSecret)) != 1 {
		return "", false
	}
	return clientID, true
}
