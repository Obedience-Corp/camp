package intent

import (
	"io"
	"os"

	"github.com/spf13/cobra"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// maxBodyFileSize is the maximum allowed size when reading body from a file or
// stdin (10 MiB). This prevents accidental pipe of huge files into intent body.
const maxBodyFileSize = 10 << 20 // 10 MiB

// resolveBody inspects --body and --body-file on cmd and returns the resolved
// body text. The second return value indicates whether any body flag was set.
//
// Mutual exclusivity: setting both --body and --body-file is a usage error.
// --body-file "-" reads from stdin with a 10 MiB cap.
func resolveBody(cmd *cobra.Command) (string, bool, error) {
	bodySet := cmd.Flags().Changed("body")
	bodyFileSet := cmd.Flags().Changed("body-file")

	if bodySet && bodyFileSet {
		return "", false, camperrors.Wrap(camperrors.ErrInvalidInput, "--body and --body-file are mutually exclusive")
	}

	if bodySet {
		body, _ := cmd.Flags().GetString("body")
		return body, true, nil
	}

	if bodyFileSet {
		path, _ := cmd.Flags().GetString("body-file")
		content, err := readBodySource(path)
		if err != nil {
			return "", false, err
		}
		return content, true, nil
	}

	return "", false, nil
}

// readBodySource reads body content from a file path or stdin (when path is "-").
// Enforces a 10 MiB size cap on input.
func readBodySource(path string) (string, error) {
	var r io.Reader

	if path == "-" {
		r = os.Stdin
	} else {
		f, err := os.Open(path)
		if err != nil {
			return "", camperrors.Wrapf(camperrors.ErrInvalidInput, "opening body file %q: %v", path, err)
		}
		defer f.Close()
		r = f
	}

	limited := io.LimitReader(r, maxBodyFileSize+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return "", camperrors.Wrap(err, "reading body input")
	}
	if len(data) > maxBodyFileSize {
		return "", camperrors.Wrap(camperrors.ErrInvalidInput, "body input exceeds maximum size of 10 MiB")
	}

	return string(data), nil
}
