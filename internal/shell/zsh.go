package shell

// generateZsh returns the zsh initialization script.
func generateZsh() string {
	return zshInit
}

const zshInit = `# Camp CLI - Zsh Integration
# Add to .zshrc: eval "$(camp shell-init zsh)"

# Check if camp is available
if ! command -v camp &> /dev/null; then
  return
fi

# Navigation function
# Usage:
#   cgo                 Interactive picker or jump to campaign root
#   cgo p               Jump to projects/
#   cgo p api           Fuzzy find "api" in projects/
#   cgo -c p ls         Run "ls" in projects/ without changing directory
cgo() {
  if [[ $# -eq 0 ]]; then
    # No args - interactive picker or jump to root
    local dest
    dest=$(camp go --print 2>/dev/null)
    if [[ -n "$dest" ]]; then
      cd "$dest"
    fi
  elif [[ "$1" == "--help" || "$1" == "-h" ]]; then
    # Show help from camp go
    camp go --help
  elif [[ "$1" == "-c" ]]; then
    # Command execution mode: cgo -c <category> <command...>
    shift
    local category="$1"
    shift
    # Build -c args for each command argument
    local args=()
    for arg in "$@"; do
      args+=(-c "$arg")
    done
    camp go "$category" "${args[@]}"
  else
    # Navigation mode
    local dest
    dest=$(camp go "$@" --print 2>/dev/null)
    if [[ -n "$dest" ]]; then
      cd "$dest"
    else
      echo "camp: not found: $*" >&2
      return 1
    fi
  fi
}

# Tab completion for cgo
_cgo() {
  local -a targets
  # Get completion candidates from camp
  if (( CURRENT == 2 )); then
    # First argument - category shortcuts and targets
    targets=(
      'p:projects directory'
      'c:corpus directory'
      'f:festivals directory'
      'a:ai_docs directory'
      'd:docs directory'
      'w:worktrees directory'
      'r:code_reviews directory'
      'pi:pipelines directory'
    )
    _describe 'category' targets
  elif (( CURRENT == 3 )); then
    # Second argument - query/target name
    local category="${words[2]}"
    local -a completions
    completions=($(camp complete "$category" 2>/dev/null))
    _describe 'target' completions
  fi
}
compdef _cgo cgo

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
