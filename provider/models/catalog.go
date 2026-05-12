package models

// catalog 全量模型清单。新增 / 修订模型时只需要修改这个变量。
//
// 数值来源：各厂商官方文档（2026-04 快照）。
//   - Anthropic: https://platform.claude.com/docs/en/about-claude/models/overview
//   - OpenAI:    https://platform.openai.com/docs/models
//   - 智谱 GLM:  https://docs.z.ai/release-notes/new-released
//   - MiniMax:   https://platform.minimax.io/docs
//   - Google:    https://ai.google.dev/gemini-api/docs/gemini-3
//   - DeepSeek:  https://api-docs.deepseek.com
//
// ContextWindow = 输入+输出合计窗口（tokens）；MaxOutput = 单次响应最大输出 tokens。
var catalog = []Info{
	// ============ Anthropic Claude 4.x ============
	// 官方当前主推：Opus 4.7 / Sonnet 4.6 / Haiku 4.5。
	{
		ID:            "claude-opus-4-7",
		Aliases:       []string{"claude-opus-4.7"},
		Vendor:        VendorAnthropic,
		ContextWindow: 1_000_000,
		MaxOutput:     128_000,
		Modalities:    []Modality{ModalityText, ModalityImage},
		Thinking:      true, // adaptive thinking
	},
	{
		ID:            "claude-opus-4-6",
		Aliases:       []string{"claude-opus-4.6"},
		Vendor:        VendorAnthropic,
		ContextWindow: 1_000_000,
		MaxOutput:     128_000,
		Modalities:    []Modality{ModalityText, ModalityImage},
		Thinking:      true,
	},
	{
		ID:            "claude-sonnet-4-6",
		Aliases:       []string{"claude-sonnet-4.6"},
		Vendor:        VendorAnthropic,
		ContextWindow: 1_000_000,
		MaxOutput:     64_000,
		Modalities:    []Modality{ModalityText, ModalityImage},
		Thinking:      true,
	},
	{
		ID:            "claude-haiku-4-5",
		Aliases:       []string{"claude-haiku-4.5", "claude-haiku-4-5-20251001"},
		Vendor:        VendorAnthropic,
		ContextWindow: 200_000,
		MaxOutput:     64_000,
		Modalities:    []Modality{ModalityText, ModalityImage},
		Thinking:      true,
	},

	// ============ OpenAI GPT-5 系列 ============
	// 5.4/5.5 默认上下文窗口 1.05M（>272K input 起按 2x/1.5x 计费）；
	// 5.3 沿用 5.0/5.1/5.2 的 400K；codex 走代码工作流，模态以文本+图为主。
	{
		ID:            "gpt-5.5",
		Aliases:       []string{"gpt-5-5"},
		Vendor:        VendorOpenAI,
		ContextWindow: 1_050_000,
		MaxOutput:     128_000,
		Modalities:    []Modality{ModalityText, ModalityImage},
		Thinking:      true,
	},
	{
		ID:            "gpt-5.4",
		Aliases:       []string{"gpt-5-4"},
		Vendor:        VendorOpenAI,
		ContextWindow: 1_050_000,
		MaxOutput:     128_000,
		Modalities:    []Modality{ModalityText, ModalityImage},
		Thinking:      true,
	},
	{
		ID:            "gpt-5.3",
		Aliases:       []string{"gpt-5-3"},
		Vendor:        VendorOpenAI,
		ContextWindow: 400_000,
		MaxOutput:     128_000,
		Modalities:    []Modality{ModalityText, ModalityImage},
		Thinking:      true,
	},
	{
		// Codex CLI 默认模型；当前指向 gpt-5.3-codex 快照
		ID:            "gpt-5-codex",
		Aliases:       []string{"gpt-5.3-codex", "gpt-5-3-codex"},
		Vendor:        VendorOpenAI,
		ContextWindow: 400_000,
		MaxOutput:     128_000,
		Modalities:    []Modality{ModalityText, ModalityImage},
		Thinking:      true,
	},

	// ============ 智谱 GLM-5 系列 ============
	// GLM-5 / GLM-5.1 为纯文本；多模态走 GLM-5V-Turbo。
	{
		ID:            "glm-5.1",
		Aliases:       []string{"glm-5-1"},
		Vendor:        VendorZhipu,
		ContextWindow: 203_000,
		MaxOutput:     131_000,
		Modalities:    []Modality{ModalityText},
		Thinking:      true,
	},
	{
		ID:            "glm-5",
		Aliases:       []string{"glm-5.0", "glm-5-0"},
		Vendor:        VendorZhipu,
		ContextWindow: 203_000,
		MaxOutput:     131_000,
		Modalities:    []Modality{ModalityText},
		Thinking:      true,
	},
	{
		// 智谱多模态编码模型（vision + text + video，744B MoE）
		ID:            "glm-5v-turbo",
		Aliases:       []string{"glm-5v"},
		Vendor:        VendorZhipu,
		ContextWindow: 200_000,
		MaxOutput:     131_000,
		Modalities:    []Modality{ModalityText, ModalityImage, ModalityVideo},
		Thinking:      true,
	},

	// ============ MiniMax ============
	// M2.7 为 MiniMax 新一代 agent 模型，纯文本、混合 reasoning。
	{
		ID:            "minimax-m2.7",
		Aliases:       []string{"minimax-2.7", "MiniMax-M2.7"},
		Vendor:        VendorMiniMax,
		ContextWindow: 205_000,
		MaxOutput:     131_072,
		Modalities:    []Modality{ModalityText},
		Thinking:      true,
	},

	// ============ Google Gemini 3 ============
	// Gemini 3 全系原生多模态（text/image/video/audio/pdf）。
	{
		ID:            "gemini-3-pro",
		Aliases:       []string{"gemini-3", "gemini-3-pro-preview"},
		Vendor:        VendorGoogle,
		ContextWindow: 1_000_000,
		MaxOutput:     65_536,
		Modalities:    []Modality{ModalityText, ModalityImage, ModalityAudio, ModalityVideo},
		Thinking:      true,
	},
	{
		ID:            "gemini-3.1-pro",
		Aliases:       []string{"gemini-3-1-pro", "gemini-3.1-pro-preview"},
		Vendor:        VendorGoogle,
		ContextWindow: 1_000_000,
		MaxOutput:     65_536,
		Modalities:    []Modality{ModalityText, ModalityImage, ModalityAudio, ModalityVideo},
		Thinking:      true,
	},
	{
		ID:            "gemini-3-flash",
		Aliases:       []string{"gemini-3-flash-preview"},
		Vendor:        VendorGoogle,
		ContextWindow: 1_000_000,
		MaxOutput:     65_536,
		Modalities:    []Modality{ModalityText, ModalityImage, ModalityAudio, ModalityVideo},
		Thinking:      true,
	},

	// ============ xAI Grok ============
	// 当前推荐 grok-4.20（2026-03 GA）；grok-4-fast 为低价高吞吐变体。
	{
		ID:            "grok-4.20",
		Aliases:       []string{"grok-4-20", "grok-4"},
		Vendor:        VendorXAI,
		ContextWindow: 2_000_000,
		MaxOutput:     32_000,
		Modalities:    []Modality{ModalityText, ModalityImage},
		Thinking:      true,
	},
	{
		ID:            "grok-4-fast",
		Aliases:       []string{"grok-4.1-fast"},
		Vendor:        VendorXAI,
		ContextWindow: 2_000_000,
		MaxOutput:     32_000,
		Modalities:    []Modality{ModalityText, ModalityImage},
		Thinking:      true,
	},

	// ============ 阿里 Qwen3 ============
	// qwen3-max 主打 agent / 工具调用；qwen3-coder-plus 长上下文代码场景。
	{
		ID:            "qwen3-max",
		Aliases:       []string{"qwen-max"},
		Vendor:        VendorAlibaba,
		ContextWindow: 262_144,
		MaxOutput:     32_768,
		Modalities:    []Modality{ModalityText, ModalityImage},
		Thinking:      true,
	},
	{
		ID:            "qwen3-coder-plus",
		Aliases:       []string{"qwen-coder-plus"},
		Vendor:        VendorAlibaba,
		ContextWindow: 1_000_000,
		MaxOutput:     65_536,
		Modalities:    []Modality{ModalityText, ModalityImage},
		Thinking:      true,
	},
	{
		ID:            "qwen3-vl-plus",
		Aliases:       []string{"qwen-vl-plus"},
		Vendor:        VendorAlibaba,
		ContextWindow: 262_144,
		MaxOutput:     32_768,
		Modalities:    []Modality{ModalityText, ModalityImage, ModalityVideo},
		Thinking:      true,
	},

	// ============ 月之暗面 Kimi ============
	// K2.6（2026-04-20）原生支持 thinking + instant 双模式。
	{
		ID:            "kimi-k2.6",
		Aliases:       []string{"kimi-k2-6", "kimi-k2"},
		Vendor:        VendorMoonshot,
		ContextWindow: 256_000,
		MaxOutput:     65_536,
		Modalities:    []Modality{ModalityText, ModalityImage},
		Thinking:      true,
	},

	// ============ DeepSeek ============
	// V4 系列原生 1M 上下文、384K 单次输出，纯文本。
	{
		ID:            "deepseek-v4-pro",
		Aliases:       []string{"deepseek-v4", "deepseek-reasoner"},
		Vendor:        VendorDeepSeek,
		ContextWindow: 1_000_000,
		MaxOutput:     384_000,
		Modalities:    []Modality{ModalityText},
		Thinking:      true,
	},
	{
		ID:            "deepseek-v4-flash",
		Aliases:       []string{"deepseek-chat"},
		Vendor:        VendorDeepSeek,
		ContextWindow: 1_000_000,
		MaxOutput:     384_000,
		Modalities:    []Modality{ModalityText},
		Thinking:      true,
	},
}
