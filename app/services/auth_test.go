package services

import (
	"encoding/json"
	"errors"
	"testing"
)

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
