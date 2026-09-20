package catalog

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jetbrains/lior-cli/internal/module"
	"github.com/jetbrains/lior-cli/internal/pkg"
)

func raiton(t *testing.T, data any) []byte {
	t.Helper()
	out := map[string]any{"message": "ok", "statusCode": 200, "data": data}
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func sampleModules() []CatalogModule {
	return []CatalogModule{
		{
			ID: "mod_1", Slug: "com.example.blog-manager", Domain: "com.example.blog-manager",
			Name: "Blog Manager", Version: "1.2.0", PrimaryCategory: "COMMUNICATION",
			Publisher: struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			}{ID: "pub_1", Name: "JetBrains"},
		},
		{
			ID: "mod_2", Slug: "com.analytics.visitors", Domain: "com.analytics.visitors",
			Name: "Visitor Analytics", Version: "0.4.1", PrimaryCategory: "DATA",
			Publisher: struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			}{ID: "pub_2", Name: "Acme Analytics"},
		},
	}
}

// fakeServer runs the catalog API surface over httptest.
type fakeServer struct {
	srv      *httptest.Server
	mods     []CatalogModule
	artifact []byte
	// searchData overrides what `/api/catalog/modules` returns (envelope data).
	searchData any
	// lastQuery records the last search query string.
	lastQuery string
}

func newFakeServer(t *testing.T, mods []CatalogModule, artifact []byte) *fakeServer {
	t.Helper()
	f := &fakeServer{mods: mods, artifact: artifact}
	if f.searchData == nil {
		f.searchData = map[string]any{"items": mods, "total": len(mods)}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/catalog/modules", func(w http.ResponseWriter, r *http.Request) {
		f.lastQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raiton(t, f.searchData))
	})
	mux.HandleFunc("/api/catalog/modules/", func(w http.ResponseWriter, r *http.Request) {
		ref := strings.TrimPrefix(r.URL.Path, "/api/catalog/modules/")
		for _, m := range f.mods {
			if m.ID == ref || m.Slug == ref || m.Domain == ref {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(raiton(t, m))
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":"module not found","statusCode":404,"data":null}`))
	})
	mux.HandleFunc("/artifacts/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(f.artifact)
	})

	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeServer) client() *Client {
	return &Client{HTTP: pkg.NewClient(f.srv.URL)}
}

func TestSearchPaginated(t *testing.T) {
	f := newFakeServer(t, sampleModules(), nil)

	res, err := f.client().Search(context.Background(), SearchOptions{
		Query: "analytics", Category: "DATA", Limit: 10, Offset: 2,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if res.Total != 2 {
		t.Errorf("Total = %d, want 2", res.Total)
	}
	if len(res.Items) != 2 {
		t.Fatalf("len(Items) = %d, want 2", len(res.Items))
	}
	if res.Items[0].Slug != "com.example.blog-manager" {
		t.Errorf("Items[0].Slug = %q", res.Items[0].Slug)
	}
	q := f.lastQuery
	for _, want := range []string{"q=analytics", "category=DATA", "limit=10", "offset=2"} {
		if !strings.Contains(q, want) {
			t.Errorf("query %q missing %q", q, want)
		}
	}
}

func TestSearchBareArray(t *testing.T) {
	f := newFakeServer(t, sampleModules(), nil)
	f.searchData = sampleModules() // bare array of items, no envelope

	res, err := f.client().Search(context.Background(), SearchOptions{})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(res.Items) != 2 || res.Total != 2 {
		t.Errorf("expected 2 items/total, got %d/%d", len(res.Items), res.Total)
	}
}

func TestGetModuleBySlug(t *testing.T) {
	f := newFakeServer(t, sampleModules(), nil)

	m, err := f.client().GetModule(context.Background(), "com.analytics.visitors")
	if err != nil {
		t.Fatalf("GetModule() error = %v", err)
	}
	if m.ID != "mod_2" || m.Version != "0.4.1" {
		t.Errorf("unexpected module: %+v", m)
	}
}

func TestGetModuleNotFound(t *testing.T) {
	f := newFakeServer(t, sampleModules(), nil)

	_, err := f.client().GetModule(context.Background(), "com.unknown.module")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !pkg.IsNotFound(err) {
		t.Errorf("expected a 404 error, got %v", err)
	}
}

func TestDownloadArtifact(t *testing.T) {
	blob := []byte("SenMod-bytes")
	mod := sampleModules()[0]
	f := newFakeServer(t, []CatalogModule{mod}, blob)

	got, err := f.client().DownloadArtifact(context.Background(), f.srv.URL+"/artifacts/"+mod.Slug+".SenMod")
	if err != nil {
		t.Fatalf("DownloadArtifact() error = %v", err)
	}
	if string(got) != string(blob) {
		t.Errorf("payload = %q, want %q", got, blob)
	}
}

func TestDownloadArtifactTooLarge(t *testing.T) {
	blob := make([]byte, module.MaxArchiveSize+1)
	mod := sampleModules()[0]
	f := newFakeServer(t, []CatalogModule{mod}, blob)

	_, err := f.client().DownloadArtifact(context.Background(), f.srv.URL+"/artifacts/"+mod.Slug+".SenMod")
	if err == nil || !strings.Contains(err.Error(), "maximum size") {
		t.Fatalf("expected a size error, got %v", err)
	}
}

func TestDownloadArtifactHTTPError(t *testing.T) {
	f := newFakeServer(t, sampleModules(), nil)

	_, err := f.client().DownloadArtifact(context.Background(), f.srv.URL+"/missing/file.SenMod")
	if err == nil || !strings.Contains(err.Error(), "HTTP 404") {
		t.Fatalf("expected an HTTP 404 error, got %v", err)
	}
}

func TestDownloadArtifactEmptyURL(t *testing.T) {
	f := newFakeServer(t, sampleModules(), nil)

	if _, err := f.client().DownloadArtifact(context.Background(), ""); err == nil {
		t.Fatal("expected an error for an empty artifact URL")
	}
}
