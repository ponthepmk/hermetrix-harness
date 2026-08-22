package identity

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// New returns a sortable-enough, collision-resistant local identifier with a
// human-readable prefix. It deliberately does not leak time, host, or user data.
func New(prefix string) string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		panic(fmt.Sprintf("identity: crypto/rand failed: %v", err))
	}
	return prefix + "_" + hex.EncodeToString(raw[:])
}
