package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// newID returns a random prefixed identifier (e.g. "sv-a1b2c3d4e5f6a7b8").
// It falls back to a nanosecond timestamp if the random source fails.
func newID(prefix string) string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	}
	return prefix + "-" + hex.EncodeToString(b)
}
