package agent

type ChangeKind int

const (
	ChangeAppended ChangeKind = iota
	ChangeTruncated
	ChangeFinalized
)

type Change struct {
	Kind    ChangeKind
	Index   int
	Message *Message
	Version int64
}
