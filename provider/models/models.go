// Package models 维护常见大模型的元数据：上下文窗口、最大输出、模态、thinking 等。
//
// 数值参考各厂商公开文档；快照时间约 2026-04，可能随版本演进发生变化。
// 新增 / 修订模型只需要修改 catalog.go。
package models

import "slices"

// Vendor 模型厂商。
type Vendor string

const (
	VendorAnthropic Vendor = "anthropic"
	VendorOpenAI    Vendor = "openai"
	VendorGoogle    Vendor = "google"
	VendorZhipu     Vendor = "zhipu" // 智谱 GLM
	VendorMiniMax   Vendor = "minimax"
	VendorDeepSeek  Vendor = "deepseek"
	VendorXAI       Vendor = "xai"
	VendorAlibaba   Vendor = "alibaba"  // Qwen 通义千问
	VendorMoonshot  Vendor = "moonshot" // Kimi
	VendorXiaomi    Vendor = "xiaomi"   // MiMo
)

// Modality 模态。
type Modality string

const (
	ModalityText  Modality = "text"
	ModalityImage Modality = "image"
	ModalityAudio Modality = "audio"
	ModalityVideo Modality = "video"
)

// Info 单个模型的元数据。
type Info struct {
	// ID 调用 API 时使用的完整模型 id。
	ID string
	// Aliases 等价别名（含点号写法、带后缀变体等）。大小写不敏感。
	Aliases []string
	// Vendor 模型厂商。
	Vendor Vendor
	// ContextWindow 上下文窗口大小（tokens，输入+输出合计上限）。0 表示未公开。
	ContextWindow int
	// MaxOutput 单次响应最大输出 token 数。0 表示未公开。
	MaxOutput int
	// Modalities 支持的输入模态。至少包含 ModalityText。
	Modalities []Modality
	// Thinking 是否原生支持 reasoning / extended thinking。
	Thinking bool
}

// Multimodal 是否多模态（支持文本以外的模态）。
func (i Info) Multimodal() bool {
	for _, m := range i.Modalities {
		if m != ModalityText {
			return true
		}
	}
	return false
}

// Supports 是否支持指定模态。
func (i Info) Supports(m Modality) bool {
	return slices.Contains(i.Modalities, m)
}
