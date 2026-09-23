# camp pull all — concurrent pulls with the live progress display.
#
# The fixture's remotes answer after a fixed 0.6-2.4s delay, standing in for
# the network round trip, so the pulling rows are visible. One repo has
# diverged and one is detached, so every result mark appears.
#
# Build: go build -o bin/camp ./cmd/camp
# Record (repo root, truecolor): just vhs record-color docs/demos/pull-all-progress.tape
Output out/pull-all-progress-raw.gif

Set Shell "bash"
Set FontFamily "JetBrainsMono Nerd Font"
Set FontSize 18
Set Width 1240
Set Height 860
Set Padding 22
Set Margin 0
Set BorderRadius 0
Set Framerate 30
Set TypingSpeed 80ms
Set Theme {"name":"Obedience Corp","background":"#0D0D11","foreground":"#F8F8F2","selection":"#3A2418","cursor":"#F2721C","black":"#0D0D11","red":"#FF5F57","green":"#50FA7B","yellow":"#FEBC2E","blue":"#F2721C","magenta":"#C2410C","cyan":"#FFB347","white":"#F8F8F2","brightBlack":"#A1A1AA","brightRed":"#FF5F57","brightGreen":"#50FA7B","brightYellow":"#FEBC2E","brightBlue":"#FFB347","brightMagenta":"#EA5513","brightCyan":"#FFB347","brightWhite":"#FFFFFF"}

Hide
Type "export PATH=$PWD/bin:$PATH" Enter
Type "unset NO_COLOR CLICOLOR" Enter
Type "export TERM=xterm-256color COLORTERM=truecolor COLORFGBG=15\;0" Enter
Type "export PS1='$ '" Enter
Type `eval "$(CAMP_BIN=$PWD/bin/camp bash docs/demos/fixtures/pull-all-progress-fixture.sh)"` Enter
Sleep 8s
Type "cd ${PULL_PROGRESS_FIXTURE:?fixture setup failed} || exit" Enter
Type "clear" Enter
Show

Type "camp pull all" Enter
Sleep 7s
