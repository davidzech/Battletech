package site

import (
	"bytes"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestIconRoute(t *testing.T) {
	srv, _ := newTestServer(t)
	r := get(t, srv, "/icons/vulture.png", nil)
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r.Body)
	if r.StatusCode != http.StatusOK || r.Header.Get("Content-Type") != "image/png" || !strings.Contains(r.Header.Get("Cache-Control"), "max-age") ||
		!bytes.HasPrefix(buf.Bytes(), []byte("\x89PNG\r\n\x1a\n")) || buf.Len() < 200 {
		t.Fatalf("vulture.png: %d %q %q (%d bytes)", r.StatusCode, r.Header.Get("Content-Type"), r.Header.Get("Cache-Control"), buf.Len())
	}
	if r := get(t, srv, "/icons/default_heavy.png", nil); r.StatusCode != http.StatusOK {
		t.Errorf("default_heavy.png: %d", r.StatusCode)
	}
	for _, bad := range []string{"nope.png", "VULTURE.PNG", "vulture.jpg", "vulture", "..%2f..%2fgo.mod", url.PathEscape("a/b.png"), "vulture.png%00", ".png", "x y.png"} {
		if r := get(t, srv, "/icons/"+bad, nil); r.StatusCode != http.StatusNotFound && r.StatusCode != http.StatusBadRequest {
			t.Errorf("/icons/%s: got %d, want 404", bad, r.StatusCode)
		}
	}
}
