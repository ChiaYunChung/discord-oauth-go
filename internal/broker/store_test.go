package broker

import (
	"testing"
	"time"
)

func TestStore_SetGet(t *testing.T) {
	s := NewStore(time.Hour) // 這個測試不需要背景清除，給一個很長的間隔即可
	s.Set("k1", map[string]string{"a": "1"}, time.Minute)

	v, ok := s.Get("k1")
	if !ok || v["a"] != "1" {
		t.Fatalf("want to get back the stored value, got %v ok=%v", v, ok)
	}
}

func TestStore_GetMissingKey(t *testing.T) {
	s := NewStore(time.Hour)
	if _, ok := s.Get("nope"); ok {
		t.Fatal("want ok=false for a key that was never set")
	}
}

func TestStore_Expiry(t *testing.T) {
	s := NewStore(time.Hour)
	s.Set("k1", map[string]string{"a": "1"}, 10*time.Millisecond)

	time.Sleep(30 * time.Millisecond)

	if _, ok := s.Get("k1"); ok {
		t.Fatal("want the entry to be expired")
	}
}

func TestStore_Pop(t *testing.T) {
	s := NewStore(time.Hour)
	s.Set("k1", map[string]string{"a": "1"}, time.Minute)

	v, ok := s.Pop("k1")
	if !ok || v["a"] != "1" {
		t.Fatalf("want to pop the stored value, got %v ok=%v", v, ok)
	}

	if _, ok := s.Get("k1"); ok {
		t.Fatal("want the key to be gone after Pop (one-time use)")
	}
}

func TestStore_PopExpiredReturnsNotFound(t *testing.T) {
	s := NewStore(time.Hour)
	s.Set("k1", map[string]string{"a": "1"}, 10*time.Millisecond)

	time.Sleep(30 * time.Millisecond)

	if _, ok := s.Pop("k1"); ok {
		t.Fatal("want Pop to report not-found for an expired entry")
	}
}

func TestStore_JanitorCleansUpExpiredEntries(t *testing.T) {
	s := NewStore(20 * time.Millisecond)
	s.Set("k1", map[string]string{"a": "1"}, 5*time.Millisecond)

	// 直接窺探內部 map，確認背景清除真的有把過期資料刪掉，
	// 而不只是 Get/Pop 讀取時才隱藏它。
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.RLock()
		_, exists := s.data["k1"]
		s.mu.RUnlock()
		if !exists {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("want janitor to eventually remove the expired entry from the internal map")
}
