# discord-oauth-go

一個專注於「Discord 登入 + 伺服器/角色白名單檢查」的輕量 OAuth 2.0 provider，
讓任何支援標準 OAuth2 Authorization Code flow（或叫 Generic OAuth2）的服務，
都能改用 Discord 帳號登入，並限制只有特定 Discord 伺服器（可選：特定角色）的
成員能通過驗證。

## 運作方式

```
下游服務  →  /authorize  →  Discord 登入
                                  ↓
下游服務  ←  /token  ←  /callback（檢查伺服器/角色）←  Discord 回呼
下游服務  →  /userinfo（帶 access_token）→ 回傳使用者資料
```

## 專案結構

```
cmd/discord-oauth-go/   進入點（讀環境變數、組裝 App、啟動 HTTP server）
internal/broker/        核心邏輯（設定、Discord API client、存取控制、HTTP handler）
```

`internal/broker` 是這個服務自己用的私有套件（Go 的 `internal/` 慣例：外部專案無法
import），對外只暴露 `LoadConfig`、`NewApp`、`(*App).Routes()` 幾個入口。

## 設定

### 1. `.env`（機敏資訊，不進版控）

複製 `.env.example` 並填入：

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
- `clients`：允許串接本服務的下游服務，各自一組：
  - `id` / `secret`：標準 OAuth `client_id` / `client_secret`，呼叫 `/token` 時要帶（HTTP Basic 或 form body 皆可）
  - `redirectURIs`：這個 client 的 `/authorize` redirect_uri 白名單，**只跟這個 client 自己比對**，其他 client 借用不了，達到互相隔離。留空則不檢查（僅建議本機開發使用）

`/authorize` 要求帶 `client_id`，且必須是 `clients` 裡登記過的其中一個，否則會被拒絕。

預設路徑是執行目錄下的 `config.yaml`，也可用 `CONFIG_PATH` 環境變數指定其他路徑。

## 測試

```bash
go test ./...
```

所有跟 Discord API 有關的測試都是拿假的 Discord server（test helper）替換掉真正的
Discord，不需要真實的 client id/secret。其中一組測試會實際把
`/authorize → /callback → /token → /userinfo` 整條 HTTP 路徑走一遍，是最接近
end-to-end 的測試；但因為 Discord 端是假的，真正跟 Discord 對接的部分（OAuth 頁面、
scope 授權畫面）還是需要人工登入一次驗證。

## 執行

```bash
go run ./cmd/discord-oauth-go
```

服務啟動於 `http://localhost:8080`，提供以下端點：

| 端點 | 說明 |
|---|---|
| `GET /authorize` | 下游服務導向此端點發起登入（`client_id`、`redirect_uri`、`state`） |
| `GET /callback` | 接收 Discord 回呼，檢查伺服器/角色資格，並記錄一筆 `[Audit]` log |
| `POST /token` | 下游服務用 `code` 換 `access_token`，需帶 `client_id`/`client_secret`（HTTP Basic 或 form body 皆可） |
| `GET /userinfo` | 下游服務帶 `Authorization: Bearer <access_token>` 取得使用者資料（`sub`/`username`/`name`/`email`/`groups`，有自訂頭像時再加 `picture`） |
| `GET /healthz` | 健康檢查，回傳 `{"status":"ok"}` |

### 關於 access_token

`/token` 核發的 `access_token` 是本服務自己產生的 opaque token（`crypto/rand`），**不是**使用者的原始 Discord token；本服務會在內部記錄這個 token 對應哪個 client、有效期預設 1 小時。這樣設計是為了：

- 就算 `access_token` 意外外流，拿到的人也打不了 Discord API，對 Discord 來說它毫無意義。
- `/userinfo` 才能驗證「這個 token 是不是真的從本服務的 `/authorize` → `/callback` → `/token` 這條路發出來的」，拒絕任何拿著跟本服務登入流程無關的 Discord token 就想查詢使用者 `groups`/身分資料的請求。

### 稽核 log

`/callback` 每次登入都會印一行 `[Audit] login user_id=... username=... result=allowed|denied|error groups=[...] ip=...`，方便追查誰在什麼時候用什麼身份登入過。

## Docker

```bash
docker compose up -d
```

`docker-compose.yml` 會掛載專案目錄下的 `config.yaml` 進容器，`.env` 則透過 `env_file` 帶入。
