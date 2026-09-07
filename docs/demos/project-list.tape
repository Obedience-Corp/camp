# camp project list — recency within groups and overlay filter (g types, not jumps).
#
# Record with:
#   just build quick
#   just vhs record-color docs/demos/project-list.tape
#
# Fixture: docs/demos/fixtures/project-list-selector-fixture.sh
Output out/project-list.gif

Set Shell "bash"
Set FontFamily "JetBrainsMono Nerd Font"
Set FontSize 16
Set Width 1360
Set Height 760
Set Padding 20
Set Margin 0
Set BorderRadius 0
Set Framerate 30
Set TypingSpeed 55ms
Set Theme {"name":"termcast","background":"#0d0d11","foreground":"#f8f8f2","cursor":"#F2721C","selection":"#2a2a33","black":"#16161a","red":"#ff5f57","green":"#50fa7b","yellow":"#febc2e","blue":"#57a6ff","magenta":"#bd93f9","cyan":"#8be9fd","white":"#f8f8f2","brightBlack":"#6b7280","brightRed":"#ff6e67","brightGreen":"#69ff94","brightYellow":"#ffd23f","brightBlue":"#7ab8ff","brightMagenta":"#d0a9ff","brightCyan":"#a4f0ff","brightWhite":"#ffffff"}

Hide
Type "export PATH=$PWD/bin:$PATH" Enter
Type "unset NO_COLOR CLICOLOR" Enter
Type "export TERM=xterm-256color COLORTERM=truecolor COLORFGBG=15\;0 CLICOLOR_FORCE=1 FORCE_COLOR=1" Enter
Type "export PS1='\033[38;5;208m❯\033[0m '" Enter
Type `eval "$(CAMP_BIN=$PWD/bin/camp bash docs/demos/fixtures/project-list-selector-fixture.sh)"` Enter
Sleep 2s
Type "cd $PROJECT_LIST_SELECTOR_FIXTURE" Enter
Type "clear" Enter
Show

# grok-cli is most recent: cursor starts there with no j.
Type "camp project list" Sleep 400ms Enter
Sleep 2400ms

# Overlay filter: g types into the query, does not jump.
Type "/" Sleep 600ms
Type "go" Sleep 1400ms
Sleep 1600ms

# Enter jumps the cursor row (still grok-cli if selected).
Enter
Sleep 1800ms

Type "ls" Sleep 400ms Enter
Sleep 2200ms
