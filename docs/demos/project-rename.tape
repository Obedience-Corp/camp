# camp project rename — terminal review, then apply.
#
# Fixture + record (repo root, truecolor):
#   FIXTURE=$(mktemp -d)
#   docs/demos/fixtures/project-rename-fixture.sh "$FIXTURE" ./bin/camp
#   CAMP_VHS_ROOT=$FIXTURE just vhs record-color docs/demos/project-rename.tape
Output out/project-rename.gif

Set Shell "bash"
Set FontFamily "JetBrainsMono Nerd Font"
Set FontSize 16
Set Width 1200
Set Height 720
Set Padding 20
Set Margin 0
Set BorderRadius 0
Set Framerate 30
Set TypingSpeed 55ms
Set Theme {"name":"Obedience Corp","background":"#0D0D11","foreground":"#F8F8F2","selection":"#3A2418","cursor":"#F2721C","black":"#0D0D11","red":"#FF5F57","green":"#50FA7B","yellow":"#FEBC2E","blue":"#F2721C","magenta":"#C2410C","cyan":"#FFB347","white":"#F8F8F2","brightBlack":"#A1A1AA","brightRed":"#FF5F57","brightGreen":"#50FA7B","brightYellow":"#FEBC2E","brightBlue":"#FFB347","brightMagenta":"#EA5513","brightCyan":"#FFB347","brightWhite":"#FFFFFF"}

Hide
Type "export HOME=$(cd $CAMP_VHS_ROOT/home && pwd -P)" Enter
Type "export PATH=$CAMP_VHS_ROOT/bin:$PATH" Enter
Type "unset NO_COLOR CLICOLOR" Enter
Type "export TERM=xterm-256color COLORTERM=truecolor COLORFGBG=15\;0 CLICOLOR_FORCE=1 FORCE_COLOR=1" Enter
Type "export PS1='\033[38;5;208m❯\033[0m '" Enter
Type "cd $HOME/campaign" Enter
Type "clear" Enter
Show

# Choose atlas, name it, read the plan, then rename.
Type "camp project rename" Sleep 350ms Enter
Sleep 1700ms
Enter
Sleep 800ms
Type "atlas-core"
Sleep 1100ms
Enter
Sleep 1900ms
Enter
Sleep 2600ms
