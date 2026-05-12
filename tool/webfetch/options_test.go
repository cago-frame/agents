package webfetch

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestOptions_AllSetters(t *testing.T) {
	cfg := &config{}
	customClient := &http.Client{Timeout: 5 * time.Second}
	Client(customClient)(cfg)
	Timeout(2 * time.Second)(cfg)
	MaxRedirects(3)(cfg)
	UserAgent("MyAgent/1")(cfg)
	Model("gpt-4o")(cfg)
	Serial()(cfg)
	if cfg.client != customClient {
		t.Error("Client not applied")
	}
	if cfg.timeout != 2*time.Second {
		t.Errorf("Timeout = %v", cfg.timeout)
	}
	if cfg.maxRedirects != 3 {
		t.Errorf("MaxRedirects = %d", cfg.maxRedirects)
	}
	if cfg.userAgent != "MyAgent/1" {
		t.Errorf("UserAgent = %q", cfg.userAgent)
	}
	if cfg.model != "gpt-4o" {
		t.Errorf("Model = %q", cfg.model)
	}
	if !cfg.serial {
		t.Error("Serial not applied")
	}
}

func TestNew_SerialOption(t *testing.T) {
	tl := New(Serial())
	st, ok := tl.(interface{ Serial() bool })
	if !ok {
		t.Fatal("tool does not implement Serial()")
	}
	if !st.Serial() {
		t.Error("Serial: want true")
	}
}

func TestIsSkippable(t *testing.T) {
	for _, tag := range []string{"script", "STYLE", "Nav", "aside", "footer", "noscript"} {
		if !isSkippable(tag) {
			t.Errorf("isSkippable(%q) = false, want true", tag)
		}
	}
	for _, tag := range []string{"div", "span", "p", "main"} {
		if isSkippable(tag) {
			t.Errorf("isSkippable(%q) = true, want false", tag)
		}
	}
}

func TestCollapseSpaces(t *testing.T) {
	cases := map[string]string{
		"hello world":        "hello world",
		"  hello   world  ":  "hello world",
		"a\t\tb":             "a b",
		"a\r\nb":             "a \nb",
		"&amp; encoded &lt;": "& encoded <",
		"line1\n\n\nline2":   "line1\n\n\nline2",
		"":                   "",
		"   ":                "",
	}
	for in, want := range cases {
		if got := collapseSpaces(in); got != want {
			t.Errorf("collapseSpaces(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStripTags_BasicHTML(t *testing.T) {
	body := []byte(`<html><head><style>x{}</style></head><body>
<script>alert(1)</script>
<p>Hello <b>World</b></p>
<nav>menu</nav>
<footer>fine print</footer>
</body></html>`)
	got, err := stripTags(body, "text/html; charset=utf-8")
	if err != nil {
		t.Fatalf("stripTags: %v", err)
	}
	if !strings.Contains(got, "Hello") || !strings.Contains(got, "World") {
		t.Errorf("got %q, missing visible text", got)
	}
	if strings.Contains(got, "alert(1)") || strings.Contains(got, "menu") || strings.Contains(got, "fine print") {
		t.Errorf("got %q, leaked skippable content", got)
	}
}

func TestStripTags_BadCharsetFallsBackToBytes(t *testing.T) {
	// Use a body with broken/invalid charset declaration; charset.NewReader
	// should still return *something*, but stripTags must succeed regardless.
	body := []byte(`<html><body><p>x</p></body></html>`)
	got, err := stripTags(body, "text/html; charset=unknown-bogus-1234")
	if err != nil {
		t.Fatalf("stripTags: %v", err)
	}
	if !strings.Contains(got, "x") {
		t.Errorf("got %q", got)
	}
}

func TestMatchHost(t *testing.T) {
	cases := []struct {
		host, entry string
		want        bool
	}{
		{"example.com", "example.com", true},
		{"sub.example.com", "example.com", true},
		{"notexample.com", "example.com", false},
		{"sub.example.com", "sub.example.com", true},
		{"a.b.c", "b.c", true},
		{"a.b.c", "x.b.c", false},
	}
	for _, tc := range cases {
		if got := matchHost(tc.host, tc.entry); got != tc.want {
			t.Errorf("matchHost(%q,%q) = %v, want %v", tc.host, tc.entry, got, tc.want)
		}
	}
}

func TestHTMLToText_LenientFallback(t *testing.T) {
	// Pass an empty body so html-to-markdown returns empty; stripTags should
	// also return empty; we then fall through to step 3 (raw bytes prefix).
	got := htmlToText([]byte(""), "text/html")
	if got == "" {
		t.Error("htmlToText: want non-empty fallback")
	}
}

func TestRun_ContextCanceledImmediately(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before call
	tl := New()
	if _, err := tl.Call(ctx, map[string]any{"url": "http://example.com"}); err == nil {
		t.Error("expected error on canceled ctx")
	}
}

func TestRun_InvalidURL(t *testing.T) {
	tl := New()
	out, err := tl.Call(context.Background(), map[string]any{"url": "not-a-url"})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !out.IsError {
		t.Error("expected tool error on bare-string URL")
	}
}
