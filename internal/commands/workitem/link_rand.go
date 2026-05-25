package workitem

import "crypto/rand"

// readRand is a seam so tests can inject deterministic randomness if needed.
// Default reads from crypto/rand.
var readRand = func(b []byte) (int, error) { return rand.Read(b) }
