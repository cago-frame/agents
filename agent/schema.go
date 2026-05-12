package agent

// Schema 是工具入参的 JSONSchema 描述。
// Properties 用 map[string]*Property 表达字段，Items 用 *Property 表达数组元素，
// 都可以直接用 Go 结构体字面量构造，调用方不必手写 json.RawMessage。
type Schema struct {
	Type        string               `json:"type"`
	Description string               `json:"description,omitempty"`
	Properties  map[string]*Property `json:"properties,omitempty"`
	Required    []string             `json:"required,omitempty"`
	Items       *Property            `json:"items,omitempty"`
	Enum        []any                `json:"enum,omitempty"`
}

// Property 是单个属性的 JSONSchema 节点。可嵌套（数组用 Items、对象用 Properties）。
// 当前覆盖 JSONSchema 常用关键字；如需 pattern/minLength/format 等，按需扩展。
type Property struct {
	Type        string               `json:"type,omitempty"`
	Description string               `json:"description,omitempty"`
	Enum        []any                `json:"enum,omitempty"`
	Items       *Property            `json:"items,omitempty"`
	Properties  map[string]*Property `json:"properties,omitempty"`
	Required    []string             `json:"required,omitempty"`

	// MinItems 数组最少元素数。0 表示不约束（omitempty 不输出）。
	MinItems int `json:"minItems,omitempty"`
	// MaxItems 数组最多元素数。0 表示不约束。
	MaxItems int `json:"maxItems,omitempty"`
	// AdditionalProperties 控制对象是否允许未声明的字段。
	// 用 *bool 因为 false 是常见显式约束，需要区分"未设置"和"显式 false"。
	AdditionalProperties *bool `json:"additionalProperties,omitempty"`
}
