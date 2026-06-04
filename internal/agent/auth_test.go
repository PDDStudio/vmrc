package agent

import (
	"net/http/httptest"
	"testing"
)

func TestTokenAuthorizerAcceptsMatch(t *testing.T) {
	a := TokenAuthorizer{Token: "secret"}
	req := httptest.NewRequest("GET", "/exec?token=secret", nil)
	if err := a.Authorize(req); err != nil {
		t.Fatalf("matching token should authorize, got %v", err)
	}
}

func TestTokenAuthorizerRejectsWrong(t *testing.T) {
	a := TokenAuthorizer{Token: "secret"}
	req := httptest.NewRequest("GET", "/exec?token=nope", nil)
	if err := a.Authorize(req); err == nil {
		t.Fatal("wrong token must be rejected")
	}
}

func TestTokenAuthorizerAcceptsBearerHeader(t *testing.T) {
	a := TokenAuthorizer{Token: "secret"}
	req := httptest.NewRequest("GET", "/exec", nil)
	req.Header.Set("Authorization", "Bearer secret")
	if err := a.Authorize(req); err != nil {
		t.Fatalf("matching Bearer token should authorize, got %v", err)
	}
}

func TestTokenAuthorizerRejectsWrongBearerHeader(t *testing.T) {
	a := TokenAuthorizer{Token: "secret"}
	req := httptest.NewRequest("GET", "/exec", nil)
	req.Header.Set("Authorization", "Bearer nope")
	if err := a.Authorize(req); err == nil {
		t.Fatal("wrong Bearer token must be rejected")
	}
}

// A present Authorization header takes precedence; a stale/empty query token
// must not let a bad header through.
func TestTokenAuthorizerBearerHeaderTakesPrecedence(t *testing.T) {
	a := TokenAuthorizer{Token: "secret"}
	req := httptest.NewRequest("GET", "/exec?token=secret", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	if err := a.Authorize(req); err == nil {
		t.Fatal("a present but wrong Bearer header must not fall back to the query token")
	}
}

func TestTokenAuthorizerEmptyServerTokenDeniesAll(t *testing.T) {
	a := TokenAuthorizer{Token: ""}
	req := httptest.NewRequest("GET", "/exec?token=", nil)
	if err := a.Authorize(req); err == nil {
		t.Fatal("empty server token must always deny")
	}
	req2 := httptest.NewRequest("GET", "/exec?token=anyvalue", nil)
	if err := a.Authorize(req2); err == nil {
		t.Fatal("empty server token must deny even a non-empty client token")
	}
}
