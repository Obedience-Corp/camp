# Verify interactive note capture exposes the optional destination row and
# searchable folder picker without adding a step for campaigns with no folders.

Output "/tmp/camp-intent-note-destination.gif"

Set Shell "bash"
Set FontFamily "JetBrainsMono Nerd Font"
Set FontSize 15
Set Width 1200
Set Height 760
Set Padding 16
Set Margin 0
Set BorderRadius 0
Set Framerate 24
Set TypingSpeed 35ms
Set Theme {"name":"termcast","background":"#0d0d11","foreground":"#f8f8f2","cursor":"#F2721C","selection":"#2a2a33","black":"#16161a","red":"#ff5f57","green":"#50fa7b","yellow":"#febc2e","blue":"#57a6ff","magenta":"#bd93f9","cyan":"#8be9fd","white":"#f8f8f2","brightBlack":"#6b7280","brightRed":"#ff6e67","brightGreen":"#69ff94","brightYellow":"#ffd23f","brightBlue":"#7ab8ff","brightMagenta":"#d0a9ff","brightCyan":"#a4f0ff","brightWhite":"#ffffff"}

Env "TERM" "xterm-256color"
Env "COLORTERM" "truecolor"
Env "COLORFGBG" "15;0"
Env "CAMP_THEME" "dark"
Env "CLICOLOR_FORCE" "1"
Env "FORCE_COLOR" "1"

Hide
Type "export PATH=$(git rev-parse --show-toplevel)/bin:$PATH" Enter
Type "export HOME=$(mktemp -d /tmp/camp-note-vhs-home.XXXXXX)" Enter
Type "mkdir -p $HOME/campaign && cd $HOME/campaign" Enter
Type "camp init --name note-destination-demo -d 'VHS note capture demo' -m 'Verify destination picker'" Enter
Type "camp intent notes folders add reading" Enter
Type "clear" Enter
Show

Type "camp intent note --no-commit" Enter
Sleep 1200ms
Type "Reading queue" Enter
Sleep 900ms
Type "Add the distributed systems papers." 
Sleep 700ms
Tab
Sleep 900ms
Enter
Sleep 1000ms
Type "read"
Sleep 1100ms
Down
Sleep 800ms
Enter
Sleep 900ms
Ctrl+S
Sleep 1200ms
