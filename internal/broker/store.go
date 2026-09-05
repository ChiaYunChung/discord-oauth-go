package broker

import (
	"sync"
	"time"
)

// Store 是一個附帶 TTL、會自動清除過期資料的 key-value 儲存。
// 用來取代原本永不過期的 map，避免半途放棄登入、或忘記拿 code 換 token
// 的紀錄一直留在記憶體裡。
type Store struct {
	mu   sync.RWMutex
	data map[string]storeEntry
}

type storeEntry struct {
	value     map[string]string
	expiresAt time.Time
}

// NewStore 建立一個 Store，並啟動背景清除過期資料的 goroutine。
func NewStore(cleanupInterval time.Duration) *Store {
	s := &Store{data: make(map[string]storeEntry)}
	go s.janitor(cleanupInterval)
	return s
}

// Set 寫入一筆資料，ttl 後自動視為過期。
func (s *Store) Set(key string, value map[string]string, ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = storeEntry{value: value, expiresAt: time.Now().Add(ttl)}
}

// Get 讀取一筆未過期的資料。
func (s *Store) Get(key string) (map[string]string, bool) {
	s.mu.RLock()
	entry, ok := s.data[key]
	s.mu.RUnlock()
	if !ok || time.Now().After(entry.expiresAt) {
		return nil, false
	}
	return entry.value, true
}

// Pop 取出並刪除一筆資料，用於一次性授權碼這類「只能用一次」的場景。
func (s *Store) Pop(key string) (map[string]string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.data[key]
	if ok {
		delete(s.data, key)
	}
	if !ok || time.Now().After(entry.expiresAt) {
		return nil, false
	}
	return entry.value, true
}

func (s *Store) janitor(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		s.mu.Lock()
		for k, v := range s.data {
			if now.After(v.expiresAt) {
				delete(s.data, k)
			}
		}
		s.mu.Unlock()
	}
}
