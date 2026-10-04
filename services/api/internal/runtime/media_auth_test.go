package runtime

import (
	"net/http/httptest"
	"testing"
)

func TestViduTokenAuthentication(t *testing.T) {
	req := httptest.NewRequest("GET", "https://example.test", nil)
	applyAuthHeaders(req, RequestConfig{AuthType: "token", APIKey: "fixture-token"})
	if req.Header.Get("Authorization") != "Token fixture-token" {
		t.Fatal("Token authentication was not applied")
	}
}
