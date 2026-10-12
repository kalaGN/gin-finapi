package utils

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) {
	return 0, errors.New("random source unavailable")
}

func TestGenerateSID(t *testing.T) {
	// 测试带前缀的 SID
	sid, err := GenerateSID("615", 29)
	if err != nil {
		t.Fatalf("GenerateSID() error = %v", err)
	}

	// 验证长度
	expectedLen := len("615") + 29
	if len(sid) != expectedLen {
		t.Errorf("Expected SID length %d, got %d", expectedLen, len(sid))
	}

	// 验证前缀
	if !strings.HasPrefix(sid, "615") {
		t.Errorf("Expected SID to start with '615', got %s", sid)
	}

	// 验证字符集
	for _, ch := range sid[3:] {
		if !strings.ContainsRune(charset, ch) {
			t.Errorf("Invalid character '%c' in SID", ch)
		}
	}
}

func TestGenerateSIDUniqueness(t *testing.T) {
	// 测试生成的 ID 是否唯一
	ids := make(map[string]bool)
	count := 1000

	for i := 0; i < count; i++ {
		sid, err := GenerateSID("615", 29)
		if err != nil {
			t.Fatalf("GenerateSID() error = %v", err)
		}
		if ids[sid] {
			t.Errorf("Duplicate SID generated: %s", sid)
		}
		ids[sid] = true
	}

	if len(ids) != count {
		t.Errorf("Expected %d unique IDs, got %d", count, len(ids))
	}
}

func TestGenerateSID_RandomSourceFailure(t *testing.T) {
	if _, err := generateSID("615", 29, failingReader{}); err == nil {
		t.Fatal("generateSID() error = nil, want random source error")
	}
}

func TestGenerateSID_Concurrent(t *testing.T) {
	const workers = 200

	ids := make(chan string, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sid, err := GenerateSID("615", 29)
			if err != nil {
				errs <- err
				return
			}
			ids <- sid
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)

	for err := range errs {
		t.Errorf("GenerateSID() error = %v", err)
	}
	seen := make(map[string]struct{}, workers)
	for sid := range ids {
		if _, exists := seen[sid]; exists {
			t.Errorf("duplicate SID generated: %s", sid)
		}
		seen[sid] = struct{}{}
	}
	if len(seen) != workers {
		t.Fatalf("generated %d unique SIDs, want %d", len(seen), workers)
	}
}

func BenchmarkGenerateSID(b *testing.B) {
	for i := 0; i < b.N; i++ {
		if _, err := GenerateSID("615", 29); err != nil {
			b.Fatal(err)
		}
	}
}
