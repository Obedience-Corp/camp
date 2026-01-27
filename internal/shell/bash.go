package shell

// generateBash returns the bash initialization script.
// This script is compatible with bash 3.2+ (the version shipped with macOS).
func generateBash() string {
	return bashInit
}

// bashInit is the full bash initialization script.
// It is designed to be compatible with bash 3.2 (macOS default).
// Compatibility notes:
//   - Uses [ ] instead of [[ ]] for POSIX compatibility
//   - Avoids associative arrays (declare -A)
//   - Avoids ${var,,} lowercase syntax
//   - Uses standard complete builtin syntax
const bashInit = `# Camp CLI - Bash Integration
# Add to .bashrc: eval "$(camp shell-init bash)"

# Check if camp is available
if ! command -v camp &> /dev/null; then
  return 2>/dev/null || exit 0
fi

# Navigation function
# Usage:
#   cgo                 Interactive picker or jump to campaign root
#   cgo p               Jump to projects/
#   cgo p api           Fuzzy find "api" in projects/
#   cgo -c p ls         Run "ls" in projects/ without changing directory
cgo() {
  local dest
  if [ $# -eq 0 ]; then
    # No args - jump to campaign root
    dest=$(camp go --print 2>/dev/null)
    if [ -n "$dest" ]; then
      cd "$dest" || return 1
    fi
  elif [ "$1" = "--help" ] || [ "$1" = "-h" ]; then
    # Show help from camp go
    camp go --help
  elif [ "$1" = "-c" ]; then
    # Command execution mode: cgo -c <category> <command...>
    shift
    local category="$1"
    shift
    # Build -c args for each command argument
    local args=""
    for arg in "$@"; do
      args="$args -c $arg"
    done
    eval "camp go \"$category\" $args"
  else
    # Navigation mode
    dest=$(camp go "$@" --print 2>/dev/null)
    if [ -n "$dest" ]; then
      cd "$dest" || return 1
    else
      echo "camp: not found: $*" >&2
      return 1
    fi
  fi
}

# Tab completion for cgo
# Works with bash 3.2+
_cgo_complete() {
  local cur prev
  cur="${COMP_WORDS[COMP_CWORD]}"
  prev="${COMP_WORDS[COMP_CWORD-1]}"

  # First argument - category shortcuts
  if [ "$COMP_CWORD" -eq 1 ]; then
    COMPREPLY=($(compgen -W "p c f a d w r pi" -- "$cur"))
    return
  fi

  # Second argument - get completion from camp
  local candidates
  candidates=$(camp complete "${prev}" 2>/dev/null)
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
    local commands="init go project list register unregister shell-init complete version"
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
