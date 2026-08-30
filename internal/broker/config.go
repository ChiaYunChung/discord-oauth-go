package broker

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// GuildConfig 描述一個允許通過驗證的 Discord 伺服器。
// RoleIDs 若為空，代表只要是該伺服器成員即可通過；
// 若有設定，使用者必須擁有其中至少一個角色才算通過。
// GroupName 是通過此規則後，要塞進 /userinfo 的 groups claim 的名稱，
// 供下游服務做 group 對應權限使用；留空則不產生 group。
// 同一個 guild id 可以拆成多筆規則（不同 roleIds 對應不同 groupName），
// 使用者符合多筆規則時，groups 會包含每一筆命中的 groupName。
type GuildConfig struct {
	ID        string   `yaml:"id"`
	Name      string   `yaml:"name"`
	RoleIDs   []string `yaml:"roleIds"`
	GroupName string   `yaml:"groupName"`
}

// ClientConfig 描述一個允許串接本服務的 OAuth client（下游服務）。
// RedirectURIs 是這個 client 專屬的 redirect_uri 白名單，跟其他 client 互相隔離：
// A client 不能借用 B client 登記的 redirect_uri 發起登入流程。
// 留空則不檢查該 client 的 redirect_uri（僅適合開發環境）。
type ClientConfig struct {
	ID           string   `yaml:"id"`
	Secret       string   `yaml:"secret"`
	RedirectURIs []string `yaml:"redirectURIs"`
}

// IsRedirectURIAllowed 檢查 uri 是否在這個 client 自己的白名單內。
func (c ClientConfig) IsRedirectURIAllowed(uri string) bool {
	if len(c.RedirectURIs) == 0 {
		return true
	}
	for _, allowed := range c.RedirectURIs {
		if allowed == uri {
			return true
		}
	}
	return false
}

// Config 對應 config.yaml 的內容。
type Config struct {
	// Guilds 是允許登入的伺服器清單，符合其中任一個 guild 的規則即算通過。
	Guilds []GuildConfig `yaml:"guilds"`
	// Clients 是允許串接本服務的 client 清單，每個 client 各自的 redirect_uri
	// 白名單見 ClientConfig.RedirectURIs。
	Clients []ClientConfig `yaml:"clients"`
}

// LoadConfig 讀取並驗證 YAML 設定檔。
func LoadConfig(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("讀取設定檔失敗 (%s): %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("解析設定檔失敗 (%s): %w", path, err)
	}

	if len(cfg.Guilds) == 0 {
		return nil, fmt.Errorf("設定檔 (%s) 至少要設定一個 guilds", path)
	}
	for i, g := range cfg.Guilds {
		if g.ID == "" {
			return nil, fmt.Errorf("guilds[%d] 缺少 id", i)
		}
	}
	for i, c := range cfg.Clients {
		if c.ID == "" || c.Secret == "" {
			return nil, fmt.Errorf("clients[%d] 缺少 id 或 secret", i)
		}
	}

	return &cfg, nil
}

// FindClient 依 client_id 找出對應的設定。
func (c *Config) FindClient(clientID string) (ClientConfig, bool) {
	for _, cl := range c.Clients {
		if cl.ID == clientID {
			return cl, true
		}
	}
	return ClientConfig{}, false
}
