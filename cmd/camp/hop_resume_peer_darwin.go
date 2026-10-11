package main

import (
	"net"

	"golang.org/x/sys/unix"
)

// hopSocketPathMax is the length a unix socket's path must stay under.
var hopSocketPathMax = len(unix.RawSockaddrUnix{}.Path)

// hopPeerUID returns the uid of the process listening on the other end of a
// unix connection.
func hopPeerUID(conn net.Conn) (int, bool) {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return 0, false
	}
	raw, err := unixConn.SyscallConn()
	if err != nil {
		return 0, false
	}
	var cred *unix.Xucred
	var credErr error
	if err := raw.Control(func(fd uintptr) {
		cred, credErr = unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
	}); err != nil || credErr != nil {
		return 0, false
	}
	return int(cred.Uid), true
}
