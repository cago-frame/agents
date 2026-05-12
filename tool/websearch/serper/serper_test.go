package serper_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cago-frame/agents/tool/websearch/serper"
)

func TestSerperSearchMaps(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-KEY") != "test-key" {
			t.Errorf("missing api key")
		}
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(body, &req)
		if req["q"] != "golang" {
			t.Errorf("query not forwarded: %v", req)
		}
		if int(req["num"].(float64)) != 5 {
			t.Errorf("num not forwarded: %v", req["num"])
		}
		_, _ = w.Write([]byte(`{"organic":[
			{"link":"https://a.com","title":"A","snippet":"sa","date":"2024-01-01"},
			{"link":"https://b.com","title":"B","snippet":"sb"}
		]}`))
	}))
	defer srv.Close()

	p := serper.New("test-key", serper.Endpoint(srv.URL))
	rs, err := p.Search(context.Background(), "golang", 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(rs) != 2 {
		t.Fatalf("got %d results", len(rs))
	}
	if rs[0].URL != "https://a.com" || rs[0].Title != "A" || rs[0].Snippet != "sa" || rs[0].PageAge != "2024-01-01" {
		t.Fatalf("bad mapping: %+v", rs[0])
	}
	if rs[1].PageAge != "" {
		t.Fatalf("expected empty PageAge for second result, got %q", rs[1].PageAge)
	}
}

func TestSerperHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"invalid api key"}`))
	}))
	defer srv.Close()

	p := serper.New("bad", serper.Endpoint(srv.URL))
	_, err := p.Search(context.Background(), "x", 5)
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("got %v", err)
	}
}
