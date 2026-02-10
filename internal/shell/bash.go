package shell

import "strings"

// generateBash returns the bash initialization script.
// Shortcuts are injected dynamically from config.DefaultNavigationShortcuts().
func generateBash() string {
	return strings.Replace(bashInitTemplate, "{{SHORTCUT_WORDS}}", bashShortcutWords(), 1)
}

// bashInitTemplate is the bash initialization script with a placeholder for shortcuts.
// It is designed to be compatible with bash 3.2 (macOS default).
// Compatibility notes:
//   - Uses [ ] instead of [[ ]] for POSIX compatibility
//   - Avoids associative arrays (declare -A)
//   - Avoids ${var,,} lowercase syntax
//   - Uses standard complete builtin syntax
const bashInitTemplate = `# Camp CLI - Bash Integration
# Add to .bashrc: eval "$(camp shell-init bash)"

# Check if camp is available
if ! command -v camp &> /dev/null; then
  return 2>/dev/null || exit 0
fi

# Wrap camp binary so directory-changing subcommands work natively.
# Uses "command camp" to call the real binary, avoiding recursion.
camp() {
  local dest
  case "$1" in
    switch|sw)
      shift
      dest=$(command camp switch "$@" --print)
      if [ -n "$dest" ]; then
        cd "$dest" || return 1
      fi
      ;;
    go|g)
      shift
      if [ $# -eq 0 ]; then
        dest=$(command camp go --print 2>/dev/null)
        if [ -n "$dest" ]; then
          cd "$dest" || return 1
        fi
      elif [ "$1" = "--help" ] || [ "$1" = "-h" ]; then
        command camp go --help
      elif [ "$1" = "-c" ]; then
        command camp go "$@"
      else
        dest=$(command camp go "$@" --print 2>/dev/null)
        if [ -n "$dest" ]; then
          cd "$dest" || return 1
        else
          echo "camp: not found: $*" >&2
          return 1
        fi
      fi
      ;;
    *)
      command camp "$@"
      ;;
  esac
}

# Shorthand for camp go
# Usage:
#   cgo                 Interactive picker or jump to campaign root
#   cgo p               Jump to projects/
#   cgo p api           Fuzzy find "api" in projects/
#   cgo -c p ls         Run "ls" in projects/ without changing directory
cgo() {
  camp go "$@"
}

# Tab completion for cgo
# Works with bash 3.2+
_cgo_complete() {
  local cur prev
  cur="${COMP_WORDS[COMP_CWORD]}"
  prev="${COMP_WORDS[COMP_CWORD-1]}"

  # First argument - category shortcuts
  if [ "$COMP_CWORD" -eq 1 ]; then
    COMPREPLY=($(compgen -W "{{SHORTCUT_WORDS}}" -- "$cur"))
    return
  fi

  # Second argument - fuzzy match from camp (extract names from tab-separated output)
  # NO_COLOR prevents lipgloss/termenv from querying the terminal via OSC
  # escape sequences, which would corrupt the shell's completion state.
  local candidates
  candidates=$(NO_COLOR=1 command camp complete --described "${prev}" 2>/dev/null | cut -f1)
  if [ -n "$candidates" ]; then
    COMPREPLY=($(compgen -W "$candidates" -- "$cur"))
  fi
}
complete -F _cgo_complete cgo

# Run command from campaign root
# Usage: cr <command> [args...]
cr() {
  camp run "$@"
}

# Switch campaigns
# Usage: csw [name]
csw() {
  camp switch "$@"
}

# Quick intent capture
# Usage: cint "my idea"
cint() {
  camp intent add "$@"
}

# Explore intents interactively
# Usage: cie
cie() {
  camp intent explore "$@"
}

# Tab completion for camp
_camp_complete() {
  local cur prev
  cur="${COMP_WORDS[COMP_CWORD]}"
  prev="${COMP_WORDS[COMP_CWORD-1]}"

  # First argument - commands
  if [ "$COMP_CWORD" -eq 1 ]; then
    local commands="init go switch project list register unregister shell-init complete version"
    COMPREPLY=($(compgen -W "$commands" -- "$cur"))
    return
  fi

  # Subcommand-specific completion
  case "${COMP_WORDS[1]}" in
    go)
      _cgo_complete
      ;;
    project)
      if [ "$COMP_CWORD" -eq 2 ]; then
        COMPREPLY=($(compgen -W "add list remove" -- "$cur"))
      fi
      ;;
    shell-init)
      if [ "$COMP_CWORD" -eq 2 ]; then
        COMPREPLY=($(compgen -W "zsh bash fish" -- "$cur"))
      fi
      ;;
  esac
}
complete -F _camp_complete camp
`
