package broker

import (
	"strings"
	"testing"
)

func TestGenerateRandomID_HasPrefix(t *testing.T) {
	id, err := generateRandomID("sess_")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(id, "sess_") {
		t.Fatalf("want prefix sess_, got %q", id)
	}
	// prefix + 32 bytes 轉 hex（64 個字元）
	if len(id) != len("sess_")+64 {
		t.Fatalf("unexpected id length: %d (%q)", len(id), id)
	}
}

func TestGenerateRandomID_Unpredictable(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		id, err := generateRandomID("code_")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if seen[id] {
			t.Fatalf("generated a duplicate id: %s", id)
		}
		seen[id] = true
	}
}
