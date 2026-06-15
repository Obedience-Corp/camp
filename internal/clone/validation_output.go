package clone

import "strings"

// FormatSSHError returns user-friendly SSH error guidance.
func FormatSSHError(err error) string {
	errStr := err.Error()
	if strings.Contains(errStr, "Permission denied (publickey)") {
		return `SSH key not configured for GitHub.

To verify: ssh -T git@github.com
To set up: https://docs.github.com/en/authentication/connecting-to-github-with-ssh

Alternative: Clone via HTTPS instead of SSH`
	}
	if strings.Contains(errStr, "Host key verification failed") {
		return `GitHub host key not verified.

To fix: ssh-keyscan github.com >> ~/.ssh/known_hosts`
	}
	return errStr
}
