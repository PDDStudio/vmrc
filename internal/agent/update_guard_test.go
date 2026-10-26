package agent

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func newUpdateServer(t *testing.T, current, released string) *Server {
	t.Helper()
	srv := fakeReleaseServer(t, released, makeAgentArchive(t, []byte("new")))
	s, _ := newTestServer(t)
	s.Update = UpdateConfig{Repo: "o/r", BinaryName: "vmrc-agent", Version: current, APIBase: srv.URL}
	s.ExitFunc = func(int) {}
	return s
}

func TestUpdateRefusesPinnedDowngradeWithoutFlag(t *testing.T) {
	s := newUpdateServer(t, "v0.3.0", "v0.2.0")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, newClientRequest("POST", "/update?version=v0.2.0&token=secret", nil))
	if rec.Code != http.StatusConflict {
		t.Fatalf("a remote downgrade without downgrade=1 must be refused: code=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestUpdateCheckReportsDowngradeAsUnavailable(t *testing.T) {
	// Reporting is fine: GET only reads. This guards the 409 to POST alone.
	s := newUpdateServer(t, "v0.3.0", "v0.2.0")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, newClientRequest("GET", "/update?check=1&version=v0.2.0&token=secret", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestUpdateGetRequiresCheck(t *testing.T) {
	s := newUpdateServer(t, "v0.1.0", "v0.2.0")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, newClientRequest("GET", "/update?token=secret", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("GET without check=1 must not contact GitHub: code=%d", rec.Code)
	}
}

func TestUpdateRefusesConcurrentApply(t *testing.T) {
	s := newUpdateServer(t, "v0.1.0", "v0.2.0")
	s.updating.Store(true) // an update is already in flight
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, newClientRequest("POST", "/update?token=secret", nil))
	if rec.Code != http.StatusConflict {
		t.Fatalf("a second concurrent update must be refused: code=%d", rec.Code)
	}
}
