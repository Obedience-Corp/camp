# camp org: the two-pane org browser in a 74-column terminal.
#
# The Orgs pane sizes to its longest name, so every org reads in full with
# the counts in one column, and neither pane changes width while moving.
#
# Record with:
#   just build-camp
#   just vhs record-color docs/demos/org-tui.tape
#
# Fixture: docs/demos/fixtures/org-tui-fixture.sh
Output out/org-tui-raw.gif

Set Shell "bash"
Set FontFamily "JetBrainsMono Nerd Font"
Set FontSize 27
Set Width 1360
Set Height 930
Set Padding 33
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
Type `eval "$(bash docs/demos/fixtures/org-tui-fixture.sh)"` Enter
Sleep 1s
Type "cd $ORG_TUI_HOME" Enter
Type "clear" Enter
Show

Type "camp org" Sleep 400ms Enter
Sleep 3s

# walk the orgs down to the longest name; the pane edges stay put
Type "j" Sleep 700ms
Type "j" Sleep 700ms
Type "j" Sleep 700ms
Type "j" Sleep 2500ms

# an org with mixed statuses, then into its members
Type "j" Sleep 1800ms
Type "l" Sleep 1500ms
Type "j" Sleep 700ms
Type "j" Sleep 700ms
Type "j" Sleep 2500ms

Type "h" Sleep 1500ms
Type "q" Sleep 600ms
