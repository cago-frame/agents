package codex

import (
	"crypto/rand"
	"encoding/hex"
)

func newRunID() string {
	var buf [8]byte
	_, _ = rand.Read(buf[:])
	return hex.EncodeToString(buf[:])
}
