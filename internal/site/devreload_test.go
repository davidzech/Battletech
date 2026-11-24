package site

import (
	"bufio"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/davidzech/Battletech/internal/storage"
)

// TestDevReload: without the option there is no endpoint and no script; with it, pages load the script and
// GET /dev/reload streams this process's boot ID, which differs between two servers.
func TestDevReload(t *testing.T) {
	store, err := storage.OpenSQLite(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	start := func(opts ...Option) *httptest.Server {
		srv := httptest.NewServer(NewHandler(plainScenario001Root(t), store, append(opts, WithMekBay(&fakeMekBay{}))...))
		t.Cleanup(srv.Close)
		return srv
	}
	page := func(srv *httptest.Server) string {
		res, err := http.Get(srv.URL + "/")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return string(b)
	}
	boot := func(srv *httptest.Server) string {
		res, err := http.Get(srv.URL + "/dev/reload")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if ct := res.Header.Get("Content-Type"); ct != "text/event-stream" {
			t.Fatalf("content type %q", ct)
		}
		sc := bufio.NewScanner(res.Body)
		for sc.Scan() {
			if id, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
				return id
			}
		}
		t.Fatal("no boot ID sent")
		return ""
	}

	plain := start()
	if strings.Contains(page(plain), "devreload.js") {
		t.Error("a server without -dev-reload loads the reload script")
	}
	if res, err := http.Get(plain.URL + "/dev/reload"); err != nil {
		t.Fatal(err)
	} else if res.Body.Close(); res.StatusCode != http.StatusNotFound {
		t.Errorf("GET /dev/reload without -dev-reload: %d, want 404", res.StatusCode)
	}

	a, b := start(WithDevReload()), start(WithDevReload())
	if !strings.Contains(page(a), `<script src="/static/devreload.js"></script>`) {
		t.Error("a server with -dev-reload doesn't load the reload script")
	}
	if idA, idB := boot(a), boot(b); idA == "" || idA == idB || boot(a) != idA {
		t.Errorf("boot IDs %q and %q: want one stable ID per server, different between servers", idA, idB)
	}
}
