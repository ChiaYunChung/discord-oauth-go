package broker

import (
	"crypto/rand"
	"encoding/hex"
)

// generateRandomID 產生一組不可預測的隨機字串，取代原本用時間戳當 ID 的做法
// （時間戳可被猜測，會讓 session/code 有被冒用的風險）。
func generateRandomID(prefix string) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(buf), nil
}
