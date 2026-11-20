# camp list: the camp browser in a split-pane-sized terminal.
#
# Org headers stay on screen while the list scrolls, the footer keeps the
# action keys, ? lists every key, o lists the orgs and filters by one, and
# s cycles the selected camp's status.
#
# Record with:
#   just build-camp
#   just vhs record-color docs/demos/list-tui.tape
#
# Fixture: docs/demos/fixtures/list-tui-fixture.sh
Output out/list-tui-raw.gif

Set Shell "bash"
Set FontFamily "JetBrainsMono Nerd Font"
Set FontSize 24
Set Width 1360
Set Height 1050
Set Padding 24
Set Margin 0
Set BorderRadius 0
Set Framerate 30
Set TypingSpeed 80ms
Set Theme {"name":"Obedience Corp","background":"#0D0D11","foreground":"#F8F8F2","selection":"#3A2418","cursor":"#F2721C","black":"#0D0D11","red":"#FF5F57","green":"#50FA7B","yellow":"#FEBC2E","blue":"#F2721C","magenta":"#C2410C","cyan":"#FFB347","white":"#F8F8F2","brightBlack":"#A1A1AA","brightRed":"#FF5F57","brightGreen":"#50FA7B","brightYellow":"#FEBC2E","brightBlue":"#FFB347","brightMagenta":"#EA5513","brightCyan":"#FFB347","brightWhite":"#FFFFFF"}

Hide
Type "export PATH=$PWD/bin:$PATH" Enter
Type "unset NO_COLOR CLICOLOR" Enter
Type "export TERM=xterm-256color COLORTERM=truecolor COLORFGBG=15\;0 CLICOLOR_FORCE=1 FORCE_COLOR=1" Enter
Type "export PS1='\033[38;5;208m❯\033[0m '" Enter
Type `eval "$(bash docs/demos/fixtures/list-tui-fixture.sh)"` Enter
Sleep 1s
Type "cd $LIST_TUI_HOME" Enter
Type "clear" Enter
Show

Type "camp list" Sleep 400ms Enter
Sleep 3500ms

# scroll: the org header for the rows on screen stays pinned at the top
Type "jjjjjjjjjjjjjjjjjj" Sleep 2500ms

# every key, on one screen
Type "?" Sleep 3500ms
Escape Sleep 1200ms

# the orgs, with counts; pick one to filter
Type "o" Sleep 2500ms
Type "k" Sleep 1800ms
Enter Sleep 2500ms

# cycle a camp's status
Type "j" Sleep 700ms
Type "s" Sleep 1800ms
Type "s" Sleep 1800ms
Type "s" Sleep 2000ms

# back to every org
Type "o" Sleep 1200ms
Type "j" Sleep 1200ms
Enter Sleep 2500ms
Type "q" Sleep 600ms
