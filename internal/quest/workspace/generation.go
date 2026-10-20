package workspace

import (
	"crypto/sha256"
	"encoding/hex"
)

// Generation returns the content-addressed SHA-256 hex digest of serialized
// saved bytes. Camp and the app compare this around read/merge/atomic replace.
func Generation(serialized []byte) string {
	sum := sha256.Sum256(serialized)
	return hex.EncodeToString(sum[:])
}
