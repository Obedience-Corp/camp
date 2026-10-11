//go:build !unix

package explore

import "os"

func openRegular(path string) (*os.File, error) {
	return os.Open(path)
}
