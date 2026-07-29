# camp intent explore + create — fire-brand chrome after the visual identity pass.
#
# Full truecolor termcast theme (void + #F2721C cursor).
#
# IMPORTANT: record with a color-capable env. Agent shells often set NO_COLOR=1
# and TERM=dumb which greyscales the GIF:
#
#   env -u NO_COLOR -u CLICOLOR TERM=xterm-256color COLORTERM=truecolor \
#     CLICOLOR_FORCE=1 FORCE_COLOR=1 \
#     vhs docs/demos/intent-tui-visual.tape
#
# Or: just vhs record-color docs/demos/intent-tui-visual.tape

Output docs/demos/intent-tui-visual.gif

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
# Full-color fire theme — matches fest.build / shared brand dark palette.
Set Theme {"name":"termcast","background":"#0d0d11","foreground":"#f8f8f2","cursor":"#F2721C","selection":"#2a2a33","black":"#16161a","red":"#ff5f57","green":"#50fa7b","yellow":"#febc2e","blue":"#57a6ff","magenta":"#bd93f9","cyan":"#8be9fd","white":"#f8f8f2","brightBlack":"#6b7280","brightRed":"#ff6e67","brightGreen":"#69ff94","brightYellow":"#ffd23f","brightBlue":"#7ab8ff","brightMagenta":"#d0a9ff","brightCyan":"#a4f0ff","brightWhite":"#ffffff"}

Env "TERM" "xterm-256color"
Env "COLORTERM" "truecolor"
Env "COLORFGBG" "15;0"
Env "CAMP_THEME" "dark"
Env "CLICOLOR_FORCE" "1"
Env "FORCE_COLOR" "1"

Hide
Type "export PATH=$PWD/bin:$PATH" Enter
Type "export HOME=$(mktemp -d /tmp/camp-vhs-home.XXXXXX)" Enter
Type "export TERM=xterm-256color COLORTERM=truecolor COLORFGBG=15\;0 CAMP_THEME=dark CLICOLOR_FORCE=1 FORCE_COLOR=1" Enter
Type "unset NO_COLOR CLICOLOR" Enter
Type "export PS1=$'\033[38;5;208m❯\033[0m '" Enter
Type "mkdir -p $HOME/campaign && cd $HOME/campaign" Enter
Type "camp init --name intent-visual-demo -d 'VHS visual demo' -m 'Show fire brand intent TUI'" Enter
Type "camp intent add 'Fire chrome for intent explorer' -t feature --body 'Match Festival brand header and footer.' --no-commit" Enter
Type "camp intent add 'Ship visual identity PR' -t chore --body 'VHS evidence and review.' --no-commit" Enter
Type "camp intent add 'Empty state copy polish' -t idea --body 'Press n to capture.' --no-commit" Enter
Type "clear" Enter
Show

# ── open the explorer (full color chrome) ──
Type "camp intent explore" Sleep 400ms Enter
Sleep 3200ms

# walk the list so selection + group headers show fire accent
Down Sleep 450ms
Down Sleep 450ms
Down Sleep 550ms
Sleep 1400ms

# open create flow (step pills + header chrome)
Type "n"
Sleep 2000ms

# title step
Type "Polish intent empty states"
Sleep 1000ms
Enter
Sleep 1400ms

# type step — move selection to show fire caret
Down Sleep 450ms
Down Sleep 550ms
Sleep 1000ms

# cancel create and return to list
Escape
Sleep 700ms
Escape
Sleep 1600ms

# quit explorer
Type "q"
Sleep 500ms
