# camp fresh — one checklist for sync, prune, follow-ups, and a sweep note.
#
# Record from the camp repo, after the fixture exports its variables:
#   eval "$(CAMP_BIN=$PWD/bin/camp bash docs/demos/fixtures/fresh-checklist-fixture.sh)"
#   just vhs record-color docs/demos/fresh-checklist.tape
Output out/fresh-checklist.gif

Set Shell "bash"
Set FontFamily "JetBrainsMono Nerd Font"
Set FontSize 16
Set Width 980
Set Height 720
Set Padding 20
Set Margin 0
Set BorderRadius 0
Set Framerate 30
Set TypingSpeed 60ms
Set Theme {"name":"Obedience Corp","background":"#0D0D11","foreground":"#F8F8F2","selection":"#3A2418","cursor":"#F2721C","black":"#0D0D11","red":"#FF5F57","green":"#50FA7B","yellow":"#FEBC2E","blue":"#F2721C","magenta":"#C2410C","cyan":"#FFB347","white":"#F8F8F2","brightBlack":"#A1A1AA","brightRed":"#FF5F57","brightGreen":"#50FA7B","brightYellow":"#FEBC2E","brightBlue":"#FFB347","brightMagenta":"#EA5513","brightCyan":"#FFB347","brightWhite":"#FFFFFF"}

Hide
Type "export HOME=$FRESH_CHECKLIST_HOME" Enter
Type "export PATH=$FRESH_CHECKLIST_HOME/bin:$PATH" Enter
Type "export TERM=xterm-256color COLORTERM=truecolor COLORFGBG=15\;0" Enter
Type "export PS1='\033[38;5;208m>\033[0m '" Enter
Type "cd $FRESH_CHECKLIST_PROJECT" Enter
Type "clear" Enter
Show

Type "camp fresh" Sleep 400ms Enter
Sleep 5s
