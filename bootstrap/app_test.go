package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kalaGN/gin-finapi/pkg/config"
)

func TestRun_MalformedDotEnvDoesNotLeakContent(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile(filepath.Join(".", ".env"), []byte("SECRET=\"private-secret"), 0600); err != nil {
		t.Fatal(err)
	}
	err := Run(context.Background())
	if err == nil || strings.Contains(err.Error(), "private-secret") {
		t.Fatalf("unexpected error=%v", err)
	}
}

func TestRun_InvalidConfigReturnsBeforeServing(t *testing.T) {
	t.Chdir(t.TempDir())
	old := config.Config
	t.Cleanup(func() { config.Config = old })
	t.Setenv("MONGODB_DSN", "")
	t.Setenv("MONGODB_DATABASE", "test")
	t.Setenv("REDIS_TIMEOUT", "1s")
	t.Setenv("RATE_LIMIT_REQUESTS", "100")
	t.Setenv("RATE_LIMIT_WINDOW", "1s")
	if err := Run(context.Background()); err == nil || !strings.Contains(err.Error(), "校验配置") {
		t.Fatalf("error=%v", err)
	}
}
