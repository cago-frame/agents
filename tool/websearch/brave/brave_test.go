package brave_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cago-frame/agents/tool/websearch/brave"
)

func TestBraveSearchMaps(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Subscription-Token") != "key" {
			t.Errorf("missing token")
		}
		if r.URL.Query().Get("q") != "go" {
			t.Errorf("bad q: %q", r.URL.Query().Get("q"))
		}
		if r.URL.Query().Get("count") != "4" {
			t.Errorf("bad count: %q", r.URL.Query().Get("count"))
		}
		_, _ = w.Write([]byte(`{"web":{"results":[
			{"url":"https://q.com","title":"Q","description":"sq","age":"2 days ago"}
		]}}`))
	}))
	defer srv.Close()

	p := brave.New("key", brave.Endpoint(srv.URL))
	rs, err := p.Search(context.Background(), "go", 4)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(rs) != 1 || rs[0].URL != "https://q.com" || rs[0].Snippet != "sq" || rs[0].PageAge != "2 days ago" {
		t.Fatalf("bad: %+v", rs)
	}
}

func TestBraveHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad key"}`))
	}))
	defer srv.Close()

	p := brave.New("bad", brave.Endpoint(srv.URL))
	_, err := p.Search(context.Background(), "x", 5)
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("got %v", err)
	}
}
