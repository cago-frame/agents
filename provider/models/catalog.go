package models

// catalog 全量模型清单。新增 / 修订模型时只需要修改这个变量。
//
// 数值来源：各厂商官方文档（2026-04 快照；2026-07 增补 Claude Opus 5 /
// Sonnet 5、GPT-5.6 sol·terra·luna、Kimi K3）。
//   - Anthropic: https://platform.claude.com/docs/en/about-claude/models/overview
//   - OpenAI:    https://platform.openai.com/docs/models
//   - 智谱 GLM:  https://docs.z.ai/release-notes/new-released
//   - MiniMax:   https://platform.minimax.io/docs
//   - Google:    https://ai.google.dev/gemini-api/docs/gemini-3
//   - DeepSeek:  https://api-docs.deepseek.com
//   - Moonshot:  https://platform.kimi.ai/docs
//   - Xiaomi:    https://mimo.xiaomi.com
//
// ContextWindow = 输入+输出合计窗口（tokens）；MaxOutput = 单次响应最大输出 tokens。
var catalog = []Info{
	// ============ Anthropic Claude ============
	// 官方当前主推：Fable 5 / Opus 5 / Sonnet 5 / Haiku 4.5。
	// Fable 5（2026-06-09 GA）是 Opus 之上的新档位；Mythos 5 同日发布，
	// 仅通过 Project Glasswing 限量开放（邀请制，无自助开通）。
	// Opus 4.8 及更早已转入 legacy，见下方。
	{
		ID:            "claude-fable-5",
		Vendor:        VendorAnthropic,
		ContextWindow: 1_000_000,
		MaxOutput:     128_000,
		Modalities:    []Modality{ModalityText, ModalityImage},
		Thinking:      true, // adaptive thinking（常开，不可显式关闭）
	},
	{
		ID:            "claude-mythos-5",
		Vendor:        VendorAnthropic,
		ContextWindow: 1_000_000,
		MaxOutput:     128_000,
		Modalities:    []Modality{ModalityText, ModalityImage},
		Thinking:      true, // adaptive thinking（常开）
	},
	{
		// Opus 5（2026-07-24）接替 Opus 4.8；Claude API / Claude Code 上
		// effort 参数默认 high。
		ID:            "claude-opus-5",
		Vendor:        VendorAnthropic,
		ContextWindow: 1_000_000,
		MaxOutput:     128_000,
		Modalities:    []Modality{ModalityText, ModalityImage},
		Thinking:      true, // adaptive thinking
	},
	{
		// Sonnet 5（2026-06-30）：接近 Opus 4.8 的表现，价格更低。
		ID:            "claude-sonnet-5",
		Vendor:        VendorAnthropic,
		ContextWindow: 1_000_000,
		MaxOutput:     128_000,
		Modalities:    []Modality{ModalityText, ModalityImage},
		Thinking:      true, // adaptive thinking
	},
	{
		ID:            "claude-opus-4-8",
		Aliases:       []string{"claude-opus-4.8"},
		Vendor:        VendorAnthropic,
		ContextWindow: 1_000_000,
		MaxOutput:     128_000,
		Modalities:    []Modality{ModalityText, ModalityImage},
		Thinking:      true, // adaptive thinking
	},
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
	// 5.6（2026-07-09）分 sol / terra / luna 三档，同窗口同输出上限，差别在
	// 能力档位与价格；裸 "gpt-5.6" 指向 sol。
	// 5.4/5.5 默认上下文窗口 1.05M（>272K input 起按 2x/1.5x 计费）；
	// 5.3 沿用 5.0/5.1/5.2 的 400K；codex 走代码工作流，模态以文本+图为主。
	{
		// 前沿档：复杂专业任务
		ID:            "gpt-5.6-sol",
		Aliases:       []string{"gpt-5.6", "gpt-5-6", "gpt-5-6-sol"},
		Vendor:        VendorOpenAI,
		ContextWindow: 1_050_000,
		MaxOutput:     128_000,
		Modalities:    []Modality{ModalityText, ModalityImage},
		Thinking:      true,
	},
	{
		// 均衡档：能力与成本平衡，面向常规生产负载
		ID:            "gpt-5.6-terra",
		Aliases:       []string{"gpt-5-6-terra"},
		Vendor:        VendorOpenAI,
		ContextWindow: 1_050_000,
		MaxOutput:     128_000,
		Modalities:    []Modality{ModalityText, ModalityImage},
		Thinking:      true,
	},
	{
		// 高吞吐低价档
		ID:            "gpt-5.6-luna",
		Aliases:       []string{"gpt-5-6-luna"},
		Vendor:        VendorOpenAI,
		ContextWindow: 1_050_000,
		MaxOutput:     128_000,
		Modalities:    []Modality{ModalityText, ModalityImage},
		Thinking:      true,
	},
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
	// K3（2026-07-16）：2.8T MoE，1M 上下文 + 原生视觉；thinking 常开，
	// 只能通过 reasoning_effort（low/high/max）调档，无法关闭。
	// K2.6（2026-04-20）原生支持 thinking + instant 双模式，按不同 model id 区分。
	{
		// max_completion_tokens 默认 131072，最大可设到 1048576（= 整个窗口）。
		ID:            "kimi-k3",
		Vendor:        VendorMoonshot,
		ContextWindow: 1_048_576,
		MaxOutput:     1_048_576,
		Modalities:    []Modality{ModalityText, ModalityImage, ModalityVideo},
		Thinking:      true,
	},
	{
		ID:            "kimi-k2.6",
		Aliases:       []string{"kimi-k2-6", "kimi-k2"},
		Vendor:        VendorMoonshot,
		ContextWindow: 256_000,
		MaxOutput:     65_536,
		Modalities:    []Modality{ModalityText, ModalityImage},
		Thinking:      true,
	},
	{
		// 非 thinking 快速响应变体；同窗口、同模态，Thinking=false。
		ID:            "kimi-k2.6-instant",
		Aliases:       []string{"kimi-k2-6-instant", "kimi-k2-instant"},
		Vendor:        VendorMoonshot,
		ContextWindow: 256_000,
		MaxOutput:     65_536,
		Modalities:    []Modality{ModalityText, ModalityImage},
		Thinking:      false,
	},

	// ============ 小米 MiMo ============
	// MiMo-V2.5（2026-04-22）：310B Sparse MoE，原生多模态（text/image/audio/video）。
	{
		ID:            "mimo-v2.5",
		Aliases:       []string{"mimo-v2-5", "mimo-2.5", "MiMo-V2.5"},
		Vendor:        VendorXiaomi,
		ContextWindow: 1_000_000,
		MaxOutput:     131_000,
		Modalities:    []Modality{ModalityText, ModalityImage, ModalityAudio, ModalityVideo},
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
