package shell

// generateFish returns the fish initialization script.
func generateFish() string {
	return fishInit
}

// fishInit is the full fish initialization script.
// Fish has different syntax than bash/zsh and excellent completion support.
const fishInit = `# Camp CLI - Fish Integration
# Add to config.fish: camp shell-init fish | source

# Check if camp is available
if not command -v camp &>/dev/null
    exit 0
end

# Helper to check if completing first argument
function __camp_is_first_arg
    set -l cmd (commandline -opc)
    test (count $cmd) -eq 1
end

# Wrap camp binary so directory-changing subcommands work natively.
# Uses "command camp" to call the real binary, avoiding recursion.
function camp --description "Camp CLI wrapper with directory-changing support"
    switch "$argv[1]"
        case switch sw
            set -l rest $argv[2..-1]
            set -l dest (command camp switch $rest --print)
            if test -n "$dest"
                cd $dest
            end
        case go g
            set -l rest $argv[2..-1]
            if test (count $rest) -eq 0
                set -l dest (command camp go --print 2>/dev/null)
                if test -n "$dest"
                    cd $dest
                end
            else if test "$rest[1]" = "--help" -o "$rest[1]" = "-h"
                command camp go --help
            else if test "$rest[1]" = "-c"
                command camp go $rest
            else
                set -l dest (command camp go $rest --print 2>/dev/null)
                if test -n "$dest"
                    cd $dest
                else
                    echo "camp: not found: $rest" >&2
                    return 1
                end
            end
        case '*'
            command camp $argv
    end
end

# Shorthand for camp go
# Usage:
#   cgo                 Interactive picker or jump to campaign root
#   cgo p               Jump to projects/
#   cgo p api           Fuzzy find "api" in projects/
#   cgo -c p ls         Run "ls" in projects/ without changing directory
function cgo --description "Navigate campaign directories (shorthand for camp go)"
    camp go $argv
end

# Tab completion for cgo - category shortcuts with descriptions
complete -c cgo -f  # no file completion
complete -c cgo -n "__camp_is_first_arg" -a "p" -d "projects/"
complete -c cgo -n "__camp_is_first_arg" -a "pw" -d "projects/worktrees/"
complete -c cgo -n "__camp_is_first_arg" -a "f" -d "festivals/"
complete -c cgo -n "__camp_is_first_arg" -a "a" -d "ai_docs/"
complete -c cgo -n "__camp_is_first_arg" -a "d" -d "docs/"
complete -c cgo -n "__camp_is_first_arg" -a "du" -d "dungeon/"
complete -c cgo -n "__camp_is_first_arg" -a "w" -d "workflow/"
complete -c cgo -n "__camp_is_first_arg" -a "cr" -d "workflow/code_reviews/"
complete -c cgo -n "__camp_is_first_arg" -a "pi" -d "workflow/pipelines/"
complete -c cgo -n "__camp_is_first_arg" -a "de" -d "workflow/design/"
complete -c cgo -n "__camp_is_first_arg" -a "i" -d "workflow/intents/"

# Dynamic completion from camp
complete -c cgo -n "not __camp_is_first_arg" -a "(camp complete (commandline -opc)[2..-1] 2>/dev/null)"

# Run command from campaign root
# Usage: cr <command> [args...]
function cr --description "Run command from campaign root"
    camp run $argv
end

# Quick intent capture
# Usage: cint "my idea"
function cint --description "Quick intent capture"
    camp intent add $argv
end

# Explore intents interactively
# Usage: cie
function cie --description "Explore intents interactively"
    camp intent explore $argv
end

# Tab completion for camp commands
complete -c camp -f  # no file completion by default

# Main commands
complete -c camp -n __fish_use_subcommand -a "init" -d "Initialize a new campaign"
complete -c camp -n __fish_use_subcommand -a "go" -d "Navigate to campaign directory"
complete -c camp -n __fish_use_subcommand -a "switch" -d "Switch to a different campaign"
complete -c camp -n __fish_use_subcommand -a "project" -d "Manage projects"
complete -c camp -n __fish_use_subcommand -a "list" -d "List registered campaigns"
complete -c camp -n __fish_use_subcommand -a "register" -d "Register campaign in registry"
complete -c camp -n __fish_use_subcommand -a "unregister" -d "Remove campaign from registry"
complete -c camp -n __fish_use_subcommand -a "shell-init" -d "Output shell initialization"
complete -c camp -n __fish_use_subcommand -a "complete" -d "Generate completion candidates"
complete -c camp -n __fish_use_subcommand -a "version" -d "Show version information"

# Subcommand completions
complete -c camp -n "__fish_seen_subcommand_from go" -a "(camp complete (commandline -opc)[3..-1] 2>/dev/null)"
complete -c camp -n "__fish_seen_subcommand_from shell-init" -a "zsh" -d "Zsh shell"
complete -c camp -n "__fish_seen_subcommand_from shell-init" -a "bash" -d "Bash shell"
complete -c camp -n "__fish_seen_subcommand_from shell-init" -a "fish" -d "Fish shell"

# Project subcommands
complete -c camp -n "__fish_seen_subcommand_from project" -a "add" -d "Add a project"
complete -c camp -n "__fish_seen_subcommand_from project" -a "list" -d "List projects"
complete -c camp -n "__fish_seen_subcommand_from project" -a "remove" -d "Remove a project"

# Global flags
complete -c camp -l help -s h -d "Show help"
complete -c camp -l config -d "Config file path"
complete -c camp -l no-color -d "Disable colored output"
complete -c camp -l verbose -d "Enable verbose output"
`
