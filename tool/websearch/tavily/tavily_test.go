package tavily_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cago-frame/agents/tool/websearch/tavily"
)

func TestTavilySearchMaps(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(body, &req)
		if req["api_key"] != "k" || req["query"] != "go" || int(req["max_results"].(float64)) != 3 {
			t.Errorf("bad request: %v", req)
		}
		_, _ = w.Write([]byte(`{"results":[
			{"url":"https://x.com","title":"X","content":"sx","published_date":"2024-02-01"}
		]}`))
	}))
	defer srv.Close()

	p := tavily.New("k", tavily.Endpoint(srv.URL))
	rs, err := p.Search(context.Background(), "go", 3)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(rs) != 1 || rs[0].URL != "https://x.com" || rs[0].Snippet != "sx" || rs[0].PageAge != "2024-02-01" {
		t.Fatalf("bad: %+v", rs)
	}
}

func TestTavilyHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"detail":"bad key"}`))
	}))
	defer srv.Close()

	p := tavily.New("bad", tavily.Endpoint(srv.URL))
	_, err := p.Search(context.Background(), "x", 5)
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("got %v", err)
	}
}
