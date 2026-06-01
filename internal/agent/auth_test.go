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
