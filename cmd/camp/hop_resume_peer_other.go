//go:build !linux && !darwin

package main

import "net"

// hopPeerUID cannot identify the peer here; the path rule stands alone.
func hopPeerUID(net.Conn) (int, bool) { return 0, false }
