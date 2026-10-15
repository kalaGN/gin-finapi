package request

import (
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestIDStableAndUniqueAcrossConcurrentRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const count = 1000
	ids := make(chan string, count)
	var workers sync.WaitGroup
	for range count {
		workers.Add(1)
		go func() {
			defer workers.Done()
			c, _ := gin.CreateTestContext(nil)
			id := ID(c)
			if ID(c) != id {
				t.Error("同一请求标识发生变化")
			}
			ids <- id
		}()
	}
	workers.Wait()
	close(ids)
	seen := make(map[string]bool, count)
	for id := range ids {
		if id == "" || seen[id] {
			t.Fatalf("重复或空标识: %q", id)
		}
		seen[id] = true
	}
}
