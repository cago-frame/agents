package blocks

// AudienceMask is a bitset describing which consumers a ContentBlock projects
// to. Projections share the canonical Message but each consumer filters
// by audience:
//
//   - ToUI: included in agent.RenderForDisplay output (what humans see).
//   - ToLLM: included in agent.BuildRequest output (what the model receives).
//
// A block with audience ToAll travels through both built-in projections; a
// UI-only block (e.g. DisplayTextBlock) sets ToUI and is invisible to the LLM.
type AudienceMask uint8

const (
	// ToUI marks a block as visible in RenderForDisplay output.
	ToUI AudienceMask = 1 << 0
	// ToLLM marks a block as included in BuildRequest output.
	ToLLM AudienceMask = 1 << 1

	// ToAll is the default for blocks that travel through every projection.
	ToAll = ToUI | ToLLM
)

// Has reports whether m includes every bit in want.
func (m AudienceMask) Has(want AudienceMask) bool { return m&want == want }
