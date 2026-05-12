package blocks

import (
	"encoding/json"
	"fmt"
	"sync"
)

// StoredBlock is the on-wire form of a ContentBlock: a discriminator string
// plus the block's JSON-encoded body. Hosts serializing message content
// outside the process boundary should use Encode / Decode (or EncodeAll /
// DecodeAll) to round-trip blocks without losing type information.
//
// Without this layer, a Conversation persisted as raw json.Marshal of
// []ContentBlock would come back as []map[string]any on Unmarshal, breaking
// every downstream consumer that type-asserts.
type StoredBlock struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// Factory constructs a fresh ContentBlock value of a single type from its
// JSON-encoded body. Registered once per type via RegisterFactory.
type Factory func(data []byte) (ContentBlock, error)

var (
	registryMu sync.RWMutex
	registry   = map[string]Factory{}
)

// Register installs a Factory under the given type discriminator. Panics on
// duplicate registration so misconfiguration surfaces at process start.
//
// Prefer the generic helper RegisterFactory[T] below, which infers the
// discriminator from T.Type() and removes the boilerplate of writing a
// custom Factory closure.
func Register(typ string, f Factory) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := registry[typ]; exists {
		panic("blocks: duplicate registration for type " + typ)
	}
	registry[typ] = f
}

// RegisterFactory registers T's default Factory, which json.Unmarshals into
// a zero-value T. Call once per block type, typically from a package init().
func RegisterFactory[T ContentBlock]() {
	var zero T
	typ := zero.Type()
	Register(typ, func(data []byte) (ContentBlock, error) {
		var b T
		if err := json.Unmarshal(data, &b); err != nil {
			return nil, fmt.Errorf("blocks: decode %s: %w", typ, err)
		}
		return b, nil
	})
}

// Encode serializes b to StoredBlock form. Encode -> Decode is a lossless
// round-trip provided the type was Register'd in both processes.
func Encode(b ContentBlock) (StoredBlock, error) {
	data, err := json.Marshal(b)
	if err != nil {
		return StoredBlock{}, fmt.Errorf("blocks: encode %s: %w", b.Type(), err)
	}
	return StoredBlock{Type: b.Type(), Data: data}, nil
}

// Decode reconstructs a ContentBlock from a StoredBlock using the registry.
// Returns an error if the discriminator is unknown.
func Decode(sb StoredBlock) (ContentBlock, error) {
	registryMu.RLock()
	factory, ok := registry[sb.Type]
	registryMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("blocks: unknown type %q", sb.Type)
	}
	return factory(sb.Data)
}

// EncodeAll encodes a slice of ContentBlocks. Convenience for serializing
// Message.Content.
func EncodeAll(bs []ContentBlock) ([]StoredBlock, error) {
	out := make([]StoredBlock, 0, len(bs))
	for _, b := range bs {
		sb, err := Encode(b)
		if err != nil {
			return nil, err
		}
		out = append(out, sb)
	}
	return out, nil
}

// DecodeAll decodes a slice of StoredBlocks back into typed ContentBlocks.
func DecodeAll(sbs []StoredBlock) ([]ContentBlock, error) {
	out := make([]ContentBlock, 0, len(sbs))
	for _, sb := range sbs {
		b, err := Decode(sb)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, nil
}

// MarshalJSON implements json.Marshaler for ToolResultBlock so its nested
// Content slice round-trips through the registry instead of losing block
// types to map[string]any.
func (b ToolResultBlock) MarshalJSON() ([]byte, error) {
	nested, err := EncodeAll(b.Content)
	if err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		ToolUseID string        `json:"tool_use_id"`
		Content   []StoredBlock `json:"content,omitempty"`
		IsError   bool          `json:"is_error,omitempty"`
	}{
		ToolUseID: b.ToolUseID,
		Content:   nested,
		IsError:   b.IsError,
	})
}

// UnmarshalJSON is the inverse of MarshalJSON for ToolResultBlock.
func (b *ToolResultBlock) UnmarshalJSON(data []byte) error {
	var raw struct {
		ToolUseID string        `json:"tool_use_id"`
		Content   []StoredBlock `json:"content"`
		IsError   bool          `json:"is_error"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	b.ToolUseID = raw.ToolUseID
	b.IsError = raw.IsError
	if len(raw.Content) == 0 {
		b.Content = nil
		return nil
	}
	nested, err := DecodeAll(raw.Content)
	if err != nil {
		return err
	}
	b.Content = nested
	return nil
}
