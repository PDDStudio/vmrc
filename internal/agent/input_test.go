package agent

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestParseCombo(t *testing.T) {
	tests := []struct {
		combo    string
		wantMods []uint16
		wantKey  uint16
		wantErr  bool
	}{
		{"Enter", nil, vkReturn, false},
		{"enter", nil, vkReturn, false},
		{"Esc", nil, vkEscape, false},
		{"Tab", nil, vkTab, false},
		{"F5", nil, vkF1 + 4, false},
		{"a", nil, 'A', false},
		{"7", nil, '7', false},
		{"Ctrl+C", []uint16{vkCtrl}, 'C', false},
		{"ctrl+c", []uint16{vkCtrl}, 'C', false},
		{"Alt+F4", []uint16{vkAlt}, vkF1 + 3, false},
		{"Ctrl+Shift+Esc", []uint16{vkCtrl, vkShift}, vkEscape, false},
		{"Win+D", []uint16{vkLWin}, 'D', false},
		{"", nil, 0, true},
		{"Ctrl+", nil, 0, true},
		{"Bogus", nil, 0, true},
		{"Ctrl+Nope", nil, 0, true},
		{"Notamod+C", nil, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.combo, func(t *testing.T) {
			mods, key, err := parseCombo(tt.combo)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if !reflect.DeepEqual(mods, tt.wantMods) {
				t.Errorf("mods = %v, want %v", mods, tt.wantMods)
			}
			if key != tt.wantKey {
				t.Errorf("key = %#x, want %#x", key, tt.wantKey)
			}
		})
	}
}

// fakeInjector records the last call to each method and returns a configurable
// error.
type fakeInjector struct {
	err error

	clickX, clickY int
	clickButton    string
	clickDouble    bool
	typed          string
	keyCombo       string
}

func (f *fakeInjector) Click(x, y int, button string, double bool) error {
	f.clickX, f.clickY, f.clickButton, f.clickDouble = x, y, button, double
	return f.err
}
func (f *fakeInjector) Type(text string) error { f.typed = text; return f.err }
func (f *fakeInjector) Key(combo string) error { f.keyCombo = combo; return f.err }

func serveInput(s *Server, method, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, newClientRequest(method, target, nil))
	return rec
}

func TestClickDispatches(t *testing.T) {
	fi := &fakeInjector{}
	s := &Server{Token: "secret", Injector: fi}
	rec := serveInput(s, http.MethodPost, "/click?token=secret&x=120&y=240&button=right&double=1")
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if fi.clickX != 120 || fi.clickY != 240 || fi.clickButton != "right" || !fi.clickDouble {
		t.Fatalf("got x=%d y=%d button=%q double=%t", fi.clickX, fi.clickY, fi.clickButton, fi.clickDouble)
	}
}

func TestClickRequiresToken(t *testing.T) {
	s := &Server{Token: "secret", Injector: &fakeInjector{}}
	if rec := serveInput(s, http.MethodPost, "/click?x=1&y=2"); rec.Code != http.StatusForbidden {
		t.Fatalf("code = %d, want 403", rec.Code)
	}
}

func TestClickBadCoords(t *testing.T) {
	s := &Server{Token: "secret", Injector: &fakeInjector{}}
	for _, q := range []string{"/click?token=secret&y=2", "/click?token=secret&x=nope&y=2", "/click?token=secret&x=1"} {
		if rec := serveInput(s, http.MethodPost, q); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: code = %d, want 400", q, rec.Code)
		}
	}
}

func TestTypeDispatches(t *testing.T) {
	fi := &fakeInjector{}
	s := &Server{Token: "secret", Injector: fi}
	rec := serveInput(s, http.MethodPost, "/type?token=secret&text=hello+world")
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	if fi.typed != "hello world" {
		t.Fatalf("typed = %q", fi.typed)
	}
}

func TestTypeMissingText(t *testing.T) {
	s := &Server{Token: "secret", Injector: &fakeInjector{}}
	if rec := serveInput(s, http.MethodPost, "/type?token=secret"); rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400", rec.Code)
	}
}

func TestKeyDispatches(t *testing.T) {
	fi := &fakeInjector{}
	s := &Server{Token: "secret", Injector: fi}
	rec := serveInput(s, http.MethodPost, "/key?token=secret&combo=Ctrl%2BC")
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	if fi.keyCombo != "Ctrl+C" {
		t.Fatalf("combo = %q", fi.keyCombo)
	}
}

func TestKeyMissingCombo(t *testing.T) {
	s := &Server{Token: "secret", Injector: &fakeInjector{}}
	if rec := serveInput(s, http.MethodPost, "/key?token=secret"); rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400", rec.Code)
	}
}

func TestInputNoInteractiveSessionReturns409(t *testing.T) {
	s := &Server{Token: "secret", Injector: &fakeInjector{err: ErrNoInteractiveSession}}
	if rec := serveInput(s, http.MethodPost, "/click?token=secret&x=1&y=2"); rec.Code != http.StatusConflict {
		t.Fatalf("code = %d, want 409", rec.Code)
	}
}

func TestInputOtherErrorReturns500(t *testing.T) {
	s := &Server{Token: "secret", Injector: &fakeInjector{err: errors.New("boom")}}
	if rec := serveInput(s, http.MethodPost, "/type?token=secret&text=x"); rec.Code != http.StatusInternalServerError {
		t.Fatalf("code = %d, want 500", rec.Code)
	}
}
