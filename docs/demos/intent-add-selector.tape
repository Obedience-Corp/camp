# camp intent add — recency + in-list fuzzy via the shared selector.
#
# Record with full truecolor (agent shells often set NO_COLOR=1):
#   just build quick
#   just vhs record-color docs/demos/intent-add-selector.tape
#
# Fixture: docs/demos/fixtures/intent-add-selector-fixture.sh

Output out/intent-add-selector.gif

Set Shell "bash"
Set FontFamily "JetBrainsMono Nerd Font"
Set FontSize 15
Set Width 1200
Set Height 760
Set Padding 16
Set Margin 0
Set BorderRadius 0
Set Framerate 24
Set TypingSpeed 40ms
Set Theme {"name":"termcast","background":"#0d0d11","foreground":"#f8f8f2","cursor":"#F2721C","selection":"#2a2a33","black":"#16161a","red":"#ff5f57","green":"#50fa7b","yellow":"#febc2e","blue":"#57a6ff","magenta":"#bd93f9","cyan":"#8be9fd","white":"#f8f8f2","brightBlack":"#6b7280","brightRed":"#ff6e67","brightGreen":"#69ff94","brightYellow":"#ffd23f","brightBlue":"#7ab8ff","brightMagenta":"#d0a9ff","brightCyan":"#a4f0ff","brightWhite":"#ffffff"}

Env "TERM" "xterm-256color"
Env "COLORTERM" "truecolor"
Env "COLORFGBG" "15;0"
Env "CAMP_THEME" "dark"
Env "CLICOLOR_FORCE" "1"
Env "FORCE_COLOR" "1"

Hide
Type "export PATH=$PWD/bin:$PATH" Enter
Type "unset NO_COLOR CLICOLOR" Enter
Type "export TERM=xterm-256color COLORTERM=truecolor COLORFGBG=15\;0 CAMP_THEME=dark CLICOLOR_FORCE=1 FORCE_COLOR=1" Enter
Type "export PS1=$'\033[38;5;208m❯\033[0m '" Enter
Type `eval "$(CAMP_BIN=$PWD/bin/camp bash docs/demos/fixtures/intent-add-selector-fixture.sh)"` Enter
Sleep 2s
Type "cd $INTENT_ADD_SELECTOR_FIXTURE" Enter
Type "clear" Enter
Show

Type "camp intent add" Sleep 400ms Enter
Sleep 1800ms

Type "obey"
Sleep 800ms
Enter
Sleep 1400ms

# Type step: arrows still move in navigate mode.
Down Sleep 350ms
Down Sleep 500ms
Sleep 900ms

# Separate beat: type-to-filter with no slash.
Type "b"
Sleep 1400ms
Enter
Sleep 1400ms

# Concept type: recency lands on projects, one help line, no query row yet.
Sleep 1600ms
Enter
Sleep 1800ms

# Project items: camp (most recent) at the bottom. Type-to-filter `bu`.
Type "bu"
Sleep 1600ms

# Backspace restores the recency list.
Backspace
Sleep 400ms
Backspace
Sleep 1200ms

# Filter again, then esc clears the query without cancelling.
Type "bu"
Sleep 1000ms
Escape
Sleep 1200ms

# Cancel the picker and leave the form.
Escape
Sleep 600ms
Escape
Sleep 1200ms
