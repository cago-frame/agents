package models

import "testing"

func TestGet_KnownModels(t *testing.T) {
	cases := []struct {
		id     string
		vendor Vendor
		ctxWin int
		multi  bool
	}{
		// Anthropic
		{"claude-fable-5", VendorAnthropic, 1_000_000, true},
		{"claude-mythos-5", VendorAnthropic, 1_000_000, true},
		{"claude-opus-4-8", VendorAnthropic, 1_000_000, true},
		{"claude-opus-4-7", VendorAnthropic, 1_000_000, true},
		{"claude-opus-4-6", VendorAnthropic, 1_000_000, true},
		{"claude-sonnet-4-6", VendorAnthropic, 1_000_000, true},
		{"claude-haiku-4-5", VendorAnthropic, 200_000, true},
		// OpenAI
		{"gpt-5.5", VendorOpenAI, 1_050_000, true},
		{"gpt-5.4", VendorOpenAI, 1_050_000, true},
		{"gpt-5.3", VendorOpenAI, 400_000, true},
		{"gpt-5-codex", VendorOpenAI, 400_000, true},
		// Zhipu
		{"glm-5", VendorZhipu, 203_000, false},
		{"glm-5.1", VendorZhipu, 203_000, false},
		{"glm-5v-turbo", VendorZhipu, 200_000, true},
		// MiniMax
		{"minimax-m2.7", VendorMiniMax, 205_000, false},
		// Google
		{"gemini-3-pro", VendorGoogle, 1_000_000, true},
		{"gemini-3.1-pro", VendorGoogle, 1_000_000, true},
		{"gemini-3-flash", VendorGoogle, 1_000_000, true},
		// xAI
		{"grok-4.20", VendorXAI, 2_000_000, true},
		{"grok-4-fast", VendorXAI, 2_000_000, true},
		// Qwen
		{"qwen3-max", VendorAlibaba, 262_144, true},
		{"qwen3-coder-plus", VendorAlibaba, 1_000_000, true},
		{"qwen3-vl-plus", VendorAlibaba, 262_144, true},
		// Kimi
		{"kimi-k2.6", VendorMoonshot, 256_000, true},
		{"kimi-k2.6-instant", VendorMoonshot, 256_000, true},
		// Xiaomi MiMo
		{"mimo-v2.5", VendorXiaomi, 1_000_000, true},
		// DeepSeek
		{"deepseek-v4-pro", VendorDeepSeek, 1_000_000, false},
		{"deepseek-v4-flash", VendorDeepSeek, 1_000_000, false},
	}
	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			info, ok := Get(c.id)
			if !ok {
				t.Fatalf("Get(%q): not found", c.id)
			}
			if info.Vendor != c.vendor {
				t.Errorf("vendor: got %v, want %v", info.Vendor, c.vendor)
			}
			if info.ContextWindow != c.ctxWin {
				t.Errorf("context: got %d, want %d", info.ContextWindow, c.ctxWin)
			}
			if info.Multimodal() != c.multi {
				t.Errorf("multimodal: got %v, want %v", info.Multimodal(), c.multi)
			}
			if info.MaxOutput == 0 {
				t.Error("MaxOutput is 0")
			}
			if !info.Supports(ModalityText) {
				t.Error("must support text modality")
			}
		})
	}
}

func TestGet_NotFound(t *testing.T) {
	if _, ok := Get("does-not-exist"); ok {
		t.Fatal("expected miss")
	}
}

func TestGet_CaseInsensitive(t *testing.T) {
	info, ok := Get("CLAUDE-OPUS-4-7")
	if !ok || info.ID != "claude-opus-4-7" {
		t.Fatalf("case-insensitive lookup failed: %+v ok=%v", info, ok)
	}
}

func TestGet_Aliases(t *testing.T) {
	cases := []struct {
		alias string
		id    string
	}{
		{"claude-opus-4.8", "claude-opus-4-8"},
		{"claude-opus-4.7", "claude-opus-4-7"},
		{"gemini-3", "gemini-3-pro"},
		{"glm-5.0", "glm-5"},
		{"deepseek-chat", "deepseek-v4-flash"},
		{"deepseek-reasoner", "deepseek-v4-pro"},
		{"gpt-5.3-codex", "gpt-5-codex"},
		{"minimax-2.7", "minimax-m2.7"},
		{"qwen-max", "qwen3-max"},
		{"kimi-k2", "kimi-k2.6"},
		{"kimi-k2-instant", "kimi-k2.6-instant"},
		{"grok-4", "grok-4.20"},
		{"mimo-2.5", "mimo-v2.5"},
	}
	for _, c := range cases {
		t.Run(c.alias, func(t *testing.T) {
			info, ok := Get(c.alias)
			if !ok {
				t.Fatalf("alias %q not found", c.alias)
			}
			if info.ID != c.id {
				t.Fatalf("alias %q resolved to %q, want %q", c.alias, info.ID, c.id)
			}
		})
	}
}

func TestByVendor(t *testing.T) {
	cases := []struct {
		v    Vendor
		want int
	}{
		{VendorAnthropic, 7},
		{VendorOpenAI, 4},
		{VendorZhipu, 3},
		{VendorMiniMax, 1},
		{VendorGoogle, 3},
		{VendorDeepSeek, 2},
		{VendorXAI, 2},
		{VendorAlibaba, 3},
		{VendorMoonshot, 2},
		{VendorXiaomi, 1},
	}
	for _, c := range cases {
		if n := len(ByVendor(c.v)); n != c.want {
			t.Errorf("%s: want %d, got %d", c.v, c.want, n)
		}
	}
}

func TestAll_NoDuplicateIDOrAlias(t *testing.T) {
	seen := make(map[string]string)
	for _, m := range All() {
		if prev, ok := seen[m.ID]; ok {
			t.Errorf("duplicate id %q (also in %q)", m.ID, prev)
		}
		seen[m.ID] = m.ID
		for _, a := range m.Aliases {
			if prev, ok := seen[a]; ok {
				t.Errorf("alias %q on %q collides with %q", a, m.ID, prev)
			}
			seen[a] = m.ID
		}
	}
}

func TestMustGet_PanicsOnMiss(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	_ = MustGet("nope")
}

func TestSupports(t *testing.T) {
	gemini := MustGet("gemini-3-pro")
	for _, m := range []Modality{ModalityText, ModalityImage, ModalityAudio, ModalityVideo} {
		if !gemini.Supports(m) {
			t.Errorf("gemini-3-pro should support %s", m)
		}
	}
	mm := MustGet("minimax-m2.7")
	if mm.Supports(ModalityImage) {
		t.Error("minimax-m2.7 should not support image")
	}
	dv4 := MustGet("deepseek-v4-pro")
	if dv4.Multimodal() {
		t.Error("deepseek-v4-pro should be text-only")
	}
}
