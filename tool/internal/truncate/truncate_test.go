package truncate_test

import (
	"strings"
	"testing"

	"github.com/cago-frame/agents/tool/internal/truncate"
)

func TestHeadNoOpUnderLimits(t *testing.T) {
	r := truncate.Head("a\nb\nc", truncate.Options{})
	if r.Truncated || r.Content != "a\nb\nc" {
		t.Fatalf("unexpected: %+v", r)
	}
	if r.TotalLines != 3 || r.TotalBytes != 5 {
		t.Fatalf("totals: %+v", r)
	}
}

func TestHeadCutByLines(t *testing.T) {
	r := truncate.Head("a\nb\nc\nd", truncate.Options{MaxLines: 2})
	if !r.Truncated || r.TruncatedBy != "lines" {
		t.Fatalf("expected lines truncation, got %+v", r)
	}
	if r.Content != "a\nb" || r.OutputLines != 2 {
		t.Fatalf("content: %+v", r)
	}
}

func TestHeadCutByBytes(t *testing.T) {
	// each line is 5 bytes: "aaaaa\nbbbbb\nccccc"  (total 17 bytes)
	r := truncate.Head("aaaaa\nbbbbb\nccccc", truncate.Options{MaxBytes: 11})
	if !r.Truncated || r.TruncatedBy != "bytes" {
		t.Fatalf("expected bytes truncation, got %+v", r)
	}
	if r.Content != "aaaaa\nbbbbb" {
		t.Fatalf("content: %q", r.Content)
	}
}

func TestHeadFirstLineExceeds(t *testing.T) {
	first := strings.Repeat("x", 100)
	r := truncate.Head(first+"\nrest", truncate.Options{MaxBytes: 50})
	if !r.FirstLineExceedsLimit || r.Content != "" {
		t.Fatalf("expected firstLine flag, got %+v", r)
	}
	if r.TruncatedBy != "bytes" || !r.Truncated {
		t.Fatalf("flags: %+v", r)
	}
}

func TestTailNoOp(t *testing.T) {
	r := truncate.Tail("a\nb", truncate.Options{})
	if r.Truncated || r.Content != "a\nb" {
		t.Fatalf("unexpected: %+v", r)
	}
}

func TestTailCutByLines(t *testing.T) {
	r := truncate.Tail("a\nb\nc\nd", truncate.Options{MaxLines: 2})
	if !r.Truncated || r.TruncatedBy != "lines" || r.Content != "c\nd" {
		t.Fatalf("expected last 2 lines, got %+v", r)
	}
}

func TestTailLastLinePartial(t *testing.T) {
	last := strings.Repeat("z", 200)
	r := truncate.Tail("first\n"+last, truncate.Options{MaxBytes: 50})
	if !r.LastLinePartial || !r.Truncated {
		t.Fatalf("expected partial last line, got %+v", r)
	}
	if len(r.Content) != 50 {
		t.Fatalf("expected exactly 50 bytes, got %d", len(r.Content))
	}
	if !strings.HasSuffix(r.Content, "z") {
		t.Fatalf("expected tail of zs, got %q", r.Content)
	}
}

func TestTailUTF8SafeCut(t *testing.T) {
	// 50 ASCII bytes followed by a multibyte char that straddles the cut.
	prefix := strings.Repeat("a", 60)
	line := prefix + "中文" // 中=3 bytes, 文=3 bytes
	r := truncate.Tail(line, truncate.Options{MaxBytes: 10})
	if !r.LastLinePartial {
		t.Fatalf("expected partial")
	}
	// must not start mid-rune: first byte must not be 10xxxxxx
	if len(r.Content) > 0 && r.Content[0]&0xc0 == 0x80 {
		t.Fatalf("cut started in middle of rune: %q", r.Content)
	}
}

func TestLineUnchanged(t *testing.T) {
	out, trunc := truncate.Line("abc", 10)
	if trunc || out != "abc" {
		t.Fatalf("unexpected: %q %v", out, trunc)
	}
}

func TestLineTruncated(t *testing.T) {
	out, trunc := truncate.Line(strings.Repeat("x", 1000), 500)
	if !trunc {
		t.Fatalf("expected truncation")
	}
	if !strings.HasSuffix(out, "... [truncated]") {
		t.Fatalf("missing suffix: %q", out)
	}
}

func TestFormatSize(t *testing.T) {
	cases := []struct {
		in   int
		want string
	}{
		{500, "500B"},
		{1024, "1.0KB"},
		{50 * 1024, "50.0KB"},
		{1024*1024 + 100, "1.0MB"},
	}
	for _, c := range cases {
		if got := truncate.FormatSize(c.in); got != c.want {
			t.Errorf("FormatSize(%d)=%q want %q", c.in, got, c.want)
		}
	}
}
