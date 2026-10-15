package services

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestSnapshotValidation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		expires int64
		denied  bool
	}{
		{"永久", 1, 0, false}, {"未到期", 1, 101, false},
		{"边界", 1, 100, true}, {"已到期", 1, 99, true},
		{"禁用", 0, 0, true}, {"非法状态", 2, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Unix(100, 999999999)
			credential := Credential{Status: tc.status, ExpiresAt: tc.expires}
			access := ProductAccess{Status: tc.status, ExpiresAt: tc.expires}
			if err := credential.Validate(now); (err != nil) != tc.denied || err != nil && !errors.Is(err, ErrAPIKeyInvalid) {
				t.Fatalf("credential=%v", err)
			}
			if err := access.Validate(now); (err != nil) != tc.denied || err != nil && !errors.Is(err, ErrProductDenied) {
				t.Fatalf("access=%v", err)
			}
		})
	}
}

func TestAuthenticationSnapshots_JSONContract(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value interface{}
		want  string
	}{
		{"凭据", Credential{Version: 1, Secret: "test-secret", Status: 1, ExpiresAt: 100}, `{"version":1,"secret":"test-secret","status":1,"expires_at":100}`},
		{"产品授权", ProductAccess{Version: 1, Status: 1, ExpiresAt: 100}, `{"version":1,"status":1,"expires_at":100}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.value)
			if err != nil || string(data) != tc.want {
				t.Fatalf("JSON=%s 错误=%v", data, err)
			}
		})
	}
}

func TestAuthenticationErrors_RemainDistinctFromQuery(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{ErrAPIKeyInvalid, "API key is invalid"},
		{ErrAuthUnavailable, "authentication service unavailable"},
		{ErrProductDenied, "API key is not authorized for product"},
	} {
		if tc.err.Error() != tc.want || errors.Is(tc.err, ErrUnavailable) {
			t.Fatal("认证错误语义或分类发生变化")
		}
	}
}
