package project

import (
	"fmt"
	"strings"
)

// GitError provides user-friendly diagnosis and fixes for git errors.
type GitError struct {
	// Command is the git command that failed.
	Command string
	// RawOutput is the raw git error output.
	RawOutput string
	// Diagnosis is a user-friendly explanation of what went wrong.
	Diagnosis string
	// Fix contains step-by-step instructions to resolve the issue.
	Fix string
	// DocLink is an optional link to relevant documentation.
	DocLink string
}

func (e *GitError) Error() string {
	var b strings.Builder
	b.WriteString(e.Diagnosis)
	if e.Fix != "" {
		b.WriteString("\n\n")
		b.WriteString(e.Fix)
	}
	if e.DocLink != "" {
		b.WriteString("\n\n")
		b.WriteString("Documentation: ")
		b.WriteString(e.DocLink)
	}
	return b.String()
}

// DiagnoseGitError analyzes git command failures and provides helpful guidance.
func DiagnoseGitError(command, output string, exitCode int) *GitError {
	outputLower := strings.ToLower(output)

	// SSH authentication failures
	if strings.Contains(outputLower, "permission denied (publickey)") {
		return &GitError{
			Command:   command,
			RawOutput: output,
			Diagnosis: "SSH authentication failed - your SSH key is not configured or not authorized",
			Fix: `To fix this:
1. Check if you have an SSH key: ls -la ~/.ssh/id_*.pub
2. If not, generate one: ssh-keygen -t ed25519 -C "your@email.com"
3. Add the public key to your Git provider:
   - GitHub: Settings → SSH Keys → New SSH Key
   - GitLab: Preferences → SSH Keys
   - Bitbucket: Personal Settings → SSH Keys
4. Test connection: ssh -T git@github.com (or gitlab.com/bitbucket.org)
5. Retry the camp command`,
			DocLink: "https://docs.github.com/en/authentication/connecting-to-github-with-ssh",
		}
	}

	// Repository not found
	if strings.Contains(outputLower, "repository not found") ||
		strings.Contains(outputLower, "could not read from remote repository") {
		return &GitError{
			Command:   command,
			RawOutput: output,
			Diagnosis: "Repository not found or you don't have access",
			Fix: `To fix this:
1. Verify the repository URL is correct
2. Check that the repository exists on the remote server
3. Ensure you have read access to the repository
4. For private repos, verify your SSH key or credentials are configured
5. Try accessing the repository URL in your browser`,
			DocLink: "",
		}
	}

	// HTTPS credential issues
	if strings.Contains(outputLower, "could not read username") ||
		strings.Contains(outputLower, "could not read password") ||
		strings.Contains(outputLower, "authentication failed") {
		return &GitError{
			Command:   command,
			RawOutput: output,
			Diagnosis: "HTTPS authentication failed - credentials not configured",
			Fix: `To fix this:
1. Configure git credentials:
   git config --global credential.helper store
2. Or use SSH URLs instead of HTTPS (recommended):
   - SSH:   git@github.com:org/repo.git
   - HTTPS: https://github.com/org/repo.git
3. For GitHub, consider using a Personal Access Token:
   https://github.com/settings/tokens
4. Retry the camp command`,
			DocLink: "https://docs.github.com/en/get-started/getting-started-with-git/caching-your-github-credentials-in-git",
		}
	}

	// Already exists in index
	if strings.Contains(outputLower, "already exists in the index") {
		return &GitError{
			Command:   command,
			RawOutput: output,
			Diagnosis: "A submodule or file already exists at this path",
			Fix: `To fix this:
1. Check existing submodules: git submodule status
2. Remove the conflicting entry:
   git rm --cached <path>
   or
   git submodule deinit -f <path>
   git rm -f <path>
3. Retry the camp command`,
			DocLink: "",
		}
	}

	// Not a git repository
	if strings.Contains(outputLower, "not a git repository") {
		return &GitError{
			Command:   command,
			RawOutput: output,
			Diagnosis: "Current directory is not a git repository",
			Fix: `To fix this:
1. Ensure you're in a campaign directory (created with 'camp init')
2. Or navigate to an existing campaign
3. If this is a new campaign, run: camp init
4. Verify git is initialized: git status`,
			DocLink: "",
		}
	}

	// Network/connection issues
	if strings.Contains(outputLower, "could not resolve host") ||
		strings.Contains(outputLower, "failed to connect") ||
		strings.Contains(outputLower, "connection timed out") {
		return &GitError{
			Command:   command,
			RawOutput: output,
			Diagnosis: "Network connection failed",
			Fix: `To fix this:
1. Check your internet connection
2. Verify the Git provider's URL is correct
3. Check if your firewall is blocking git connections
4. Try again after confirming network access`,
			DocLink: "",
		}
	}

	// Fatal errors (generic)
	if strings.Contains(outputLower, "fatal:") {
		return &GitError{
			Command:   command,
			RawOutput: output,
			Diagnosis: "Git command failed",
			Fix: fmt.Sprintf(`Git error details:
%s

Please check the error message above and:
1. Verify your git configuration: git config --list
2. Ensure the repository URL is correct
3. Check that you have necessary permissions`, output),
			DocLink: "",
		}
	}

	// Generic git error
	return &GitError{
		Command:   command,
		RawOutput: output,
		Diagnosis: "Git operation failed",
		Fix: fmt.Sprintf(`Command failed with exit code %d:
%s

Please check the error message and verify:
1. Git is installed: git --version
2. You have necessary permissions
3. The repository URL is valid`, exitCode, output),
		DocLink: "",
	}
}
