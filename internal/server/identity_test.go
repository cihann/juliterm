package server

import (
	"bytes"
	"encoding/json"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBadge(t *testing.T) {
	for name, want := range map[string]string{
		"macstudio36": "36",
		"macstudio84": "84",
		"build1234":   "234",
		"laptop":      "LA",
		"mac-mini":    "MA",
		"---":         "J",
	} {
		if got := badge(name); got != want {
			t.Errorf("badge(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestIconsDifferPerMachine(t *testing.T) {
	a, b := renderIcon("macstudio36", 180), renderIcon("macstudio84", 180)
	if bytes.Equal(a, b) {
		t.Fatal("two machines got identical icons")
	}
	img, err := png.Decode(bytes.NewReader(a))
	if err != nil {
		t.Fatal(err)
	}
	if got := img.Bounds().Dx(); got != 180 {
		t.Fatalf("icon width = %d, want 180", got)
	}
}

func TestIdentityAndManifestArePublic(t *testing.T) {
	s := newTestServer(newMockManager())
	s.SetName("macstudio84")
	mux := http.NewServeMux()
	s.setupRoutes(mux)

	for _, path := range []string{"/api/identity", "/manifest.webmanifest", "/apple-touch-icon.png", "/icon-512.png"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s without login = %d, want 200", path, rec.Code)
		}
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/manifest.webmanifest", nil))
	var m struct {
		ID        string `json:"id"`
		ShortName string `json:"short_name"`
		Display   string `json:"display"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if m.ID != "/?juli=macstudio84" || m.ShortName != "macstudio84" || m.Display != "standalone" {
		t.Errorf("manifest = %+v", m)
	}
}
