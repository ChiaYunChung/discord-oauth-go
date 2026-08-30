# discord-oauth-go

一個專注於「Discord 登入 + 伺服器/角色白名單檢查」的輕量 OAuth 2.0 provider，
可讓 Portainer、LiteLLM 等支援 Generic OAuth 的服務，改用 Discord 帳號登入，
並限制只有特定 Discord 伺服器（可選：特定角色）的成員能通過驗證。

## 運作方式

```
Client (Portainer/LiteLLM)  →  /authorize  →  Discord 登入
                                                    ↓
Client  ←  /token  ←  /callback（檢查伺服器/角色）←  Discord 回呼
Client  →  /userinfo（帶 access_token）→ 回傳使用者資料
```

## 設定

### 1. `.env`（機敏資訊，不進版控）

複製 `.env.example`（若無則自行建立）並填入：

```env
DISCORD_CLIENT_ID=你的 Discord Application Client ID
DISCORD_CLIENT_SECRET=你的 Discord Application Client Secret
DISCORD_REDIRECT_URI=https://your-broker.example.com/callback
```

這三個對應的是**本服務**在 Discord Developer Portal 註冊的 OAuth App，
需與 Discord 後台設定的 Redirect URI 完全一致。

### 2. `config.yaml`（白名單設定，不進版控）

複製 `config.example.yaml` 為 `config.yaml`，依需求修改：

- `guilds`：允許登入的 Discord 伺服器清單。符合清單中**任一個** guild 的規則即可通過：
  - `roleIds` 留空 → 只要是該伺服器成員即可
  - `roleIds` 有設定 → 使用者需擁有其中**至少一個**角色
  - `groupName`（選填）→ 命中該規則時塞進 `/userinfo` 的 `groups` claim。同一個 guild id 可拆成多筆規則，讓不同角色對應不同 group（見 `config.example.yaml`）
- `allowedRedirectURIs`：`/authorize` 允許導回的 redirect_uri 白名單（防止 open redirect）。留空則不檢查，僅建議本機開發使用。
- `clients`：允許呼叫 `/token` 端點的 client（例如 Portainer、LiteLLM），各自一組 `id` + `secret`。

預設路徑是執行目錄下的 `config.yaml`，也可用 `CONFIG_PATH` 環境變數指定其他路徑。

## 測試

```bash
go test ./...
```

所有跟 Discord API 有關的測試（`/oauth2/token`、`/users/@me`、`/users/@me/guilds`…）都是拿假的
Discord server（`discord_mock_test.go`）替換掉真正的 Discord，不需要真實的 client id/secret。
`handlers_test.go` 會實際把 `/authorize → /callback → /token → /userinfo` 整條 HTTP 路徑走一遍，
是最接近 end-to-end 的測試；但因為 Discord 端是假的，真正跟 Discord 對接的部分（OAuth 頁面、
scope 授權畫面）還是需要人工登入一次驗證。

## 執行

```bash
go run .
```

服務啟動於 `http://localhost:8080`，提供以下端點：

| 端點 | 說明 |
|---|---|
| `GET /authorize` | Client 導向此端點發起登入（`redirect_uri`、`state`、`client_id`） |
| `GET /callback` | 接收 Discord 回呼，檢查伺服器/角色資格，並記錄一筆 `[Audit]` log |
| `POST /token` | Client 用 `code` 換 `access_token`，需帶 `client_id`/`client_secret`（HTTP Basic 或 form body 皆可） |
| `GET /userinfo` | Client 帶 `Authorization: Bearer <access_token>` 取得使用者資料（`sub`/`username`/`name`/`email`/`groups`，有自訂頭像時再加 `picture`） |
| `GET /healthz` | 健康檢查，回傳 `{"status":"ok"}` |

### 稽核 log

`/callback` 每次登入都會印一行 `[Audit] login user_id=... username=... result=allowed|denied|error groups=[...] ip=...`，方便追查誰在什麼時候用什麼身份登入過。

## Docker

```bash
docker compose up -d
```

`docker-compose.yml` 會掛載專案目錄下的 `config.yaml` 進容器，`.env` 則透過 `env_file` 帶入。
