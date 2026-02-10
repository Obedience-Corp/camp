package shell

// generateZsh returns the zsh initialization script.
// Shortcuts are injected dynamically from config.DefaultNavigationShortcuts().
func generateZsh() string {
	return zshInitPrefix + zshShortcutTargets() + zshInitSuffix
}

const zshInitPrefix = `# Camp CLI - Zsh Integration
# Add to .zshrc: eval "$(camp shell-init zsh)"

# Check if camp is available
if ! command -v camp &> /dev/null; then
  return
fi

# Wrap camp binary so directory-changing subcommands work natively.
# Uses "command camp" to call the real binary, avoiding recursion.
camp() {
  case "$1" in
    switch|sw)
      shift
      local dest
      dest=$(command camp switch "$@" --print)
      if [[ -n "$dest" ]]; then
        cd "$dest"
      fi
      ;;
    go|g)
      shift
      if [[ $# -eq 0 ]]; then
        local dest
        dest=$(command camp go --print 2>/dev/null)
        if [[ -n "$dest" ]]; then
          cd "$dest"
        fi
      elif [[ "$1" == "--help" || "$1" == "-h" ]]; then
        command camp go --help
      elif [[ "$1" == "-c" ]]; then
        command camp go "${@}"
      else
        local dest
        dest=$(command camp go "$@" --print 2>/dev/null)
        if [[ -n "$dest" ]]; then
          cd "$dest"
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
_cgo() {
  local -a targets
  # Get completion candidates from camp
  if (( CURRENT == 2 )); then
    # First argument - category shortcuts and targets
    targets=(
`

const zshInitSuffix = `
    )
    _describe 'category' targets
  elif (( CURRENT == 3 )); then
    # Second argument - fuzzy match with path descriptions
    local category="${words[2]}"
    local query="${words[3]:-}"
    local -a completions
    local line

    # Get completions with descriptions (name\tpath format)
    # NO_COLOR prevents lipgloss/termenv from querying the terminal via OSC
    # escape sequences, which would corrupt zsh's completion state machine.
    while IFS=$'\t' read -r name path; do
      [[ -n "$name" ]] && completions+=("$name:$path")
    done < <(NO_COLOR=1 command camp complete --described "$category" "$query" 2>/dev/null)

    (( ${#completions} )) && _describe 'target' completions
  fi
}
compdef _cgo cgo

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

# Camp command completion
_camp() {
  local curcontext="$curcontext" state line
  typeset -A opt_args

  _arguments -C \
    '(-h --help)'{-h,--help}'[Show help]' \
    '--config[Config file path]:file:_files' \
    '--no-color[Disable colored output]' \
    '--verbose[Enable verbose output]' \
    '1: :->command' \
    '*::arg:->args'

  case $state in
    command)
      local -a commands
      commands=(
        'init:Initialize a new campaign'
        'go:Navigate to campaign directory'
        'switch:Switch to a different campaign'
        'project:Manage projects'
        'list:List registered campaigns'
        'register:Register campaign in registry'
        'unregister:Remove campaign from registry'
        'shell-init:Output shell initialization'
        'complete:Generate completion candidates'
        'version:Show version information'
      )
      _describe 'command' commands
      ;;
    args)
      case $line[1] in
        go)
          _cgo
          ;;
        init)
          _arguments \
            '--name[Campaign name]:name' \
            '--force[Overwrite existing]' \
            '1:directory:_directories'
          ;;
        project)
          local -a project_cmds
          project_cmds=(
            'add:Add a project'
            'list:List projects'
            'remove:Remove a project'
          )
          _describe 'project command' project_cmds
          ;;
        shell-init)
          local -a shells
          shells=('zsh' 'bash' 'fish')
          _describe 'shell' shells
          ;;
        register|unregister)
          _directories
          ;;
      esac
      ;;
  esac
}
compdef _camp camp
`
