//go:build !linux && !darwin

package main

import "net"

// hopSocketPathMax is the length a unix socket's path must stay under: the
// smallest among the systems camp runs on.
var hopSocketPathMax = 104

// hopPeerUID cannot identify the peer here; the path rule stands alone.
func hopPeerUID(net.Conn) (int, bool) { return 0, false }
