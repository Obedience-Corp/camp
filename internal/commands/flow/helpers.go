package flow

import "os"

// getCwd returns the current working directory.
func getCwd() (string, error) {
	return os.Getwd()
}
