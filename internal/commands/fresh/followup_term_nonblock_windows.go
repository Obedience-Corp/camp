package fresh

import "os"

func setFollowUpPTYNonblocking(_ *os.File) error {
	// The pty dependency does not support Windows; keep the pipe fallback.
	return errFollowUpTTYUnavailable
}
