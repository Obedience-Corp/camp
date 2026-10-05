package fresh

import (
	"os"
)

func newFollowUpPTYMaster(_ *os.File) (*os.File, error) {
	// The pty dependency does not support Windows; keep the pipe fallback.
	return nil, errFollowUpTTYUnavailable
}
