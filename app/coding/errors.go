package coding

import "errors"

// ErrCompactionDisabled is returned by System.Compact when the system was
// constructed with WithoutCompaction.
var ErrCompactionDisabled = errors.New("coding: compaction disabled")
