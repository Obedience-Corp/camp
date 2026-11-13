# camp approvals — the human approval inbox browser.
#
# Shows the waiting requests beside the selected request, a long request
# scrolling inside its pane, a request that cannot be decided on this machine,
# and the confirm and reject screens. Nothing is approved: both are cancelled.
#
# Fixture + record (repo root, truecolor; fest must be on PATH):
#   FIXTURE=$(mktemp -d)
#   docs/demos/fixtures/approvals-tui-fixture.sh "$FIXTURE" ./bin/camp
#   CAMP_VHS_ROOT=$FIXTURE just vhs record-color docs/demos/approvals-tui.tape
Output out/approvals-tui.gif

Set Shell "bash"
Set FontFamily "JetBrainsMono Nerd Font"
Set FontSize 16
Set Width 1280
Set Height 620
Set Padding 18
Set Margin 0
Set BorderRadius 0
Set Framerate 30
Set TypingSpeed 55ms
Set Theme {"name":"Obedience Corp","background":"#0D0D11","foreground":"#F8F8F2","selection":"#3A2418","cursor":"#F2721C","black":"#0D0D11","red":"#FF5F57","green":"#50FA7B","yellow":"#FEBC2E","blue":"#F2721C","magenta":"#C2410C","cyan":"#FFB347","white":"#F8F8F2","brightBlack":"#A1A1AA","brightRed":"#FF5F57","brightGreen":"#50FA7B","brightYellow":"#FEBC2E","brightBlue":"#FFB347","brightMagenta":"#EA5513","brightCyan":"#FFB347","brightWhite":"#FFFFFF"}

# ── hidden setup: disposable HOME campaign already seeded by the fixture ──
Hide
Type "export HOME=$(cd $CAMP_VHS_ROOT/home && pwd -P)" Enter
Type "export PATH=$CAMP_VHS_ROOT/bin:$PATH" Enter
Type "unset NO_COLOR CLICOLOR CAMP_ROOT" Enter
Type "export GIT_AUTHOR_NAME=demo GIT_AUTHOR_EMAIL=demo@example.com GIT_COMMITTER_NAME=demo GIT_COMMITTER_EMAIL=demo@example.com" Enter
Type "export TERM=xterm-256color COLORTERM=truecolor COLORFGBG=15\;0 CLICOLOR_FORCE=1 FORCE_COLOR=1" Enter
Type "export PS1='\033[38;5;208m❯\033[0m '" Enter
Type "cd $HOME/campaign" Enter
Type "clear" Enter
Show

# Open the inbox: the list on the left, the selected request on the right.
Type "camp approvals" Sleep 400ms Enter
Sleep 3200ms

# A long request scrolls inside its own pane; the keys stay put.
Type "j" Sleep 1800ms
PageDown Sleep 1600ms
PageUp Sleep 1000ms

# A request whose source is not on this machine says so instead of offering
# keys that cannot work.
Type "j" Sleep 2600ms

# Approve shows the exact command and directory, then waits. Cancel it.
Type "k" Sleep 300ms
Type "k" Sleep 1200ms
Type "a" Sleep 3000ms
Type "n" Sleep 1200ms

# Reject asks for the reason the agent will read. Cancel that too.
Type "x" Sleep 800ms
Type "notes still describe the old flag" Sleep 1800ms
Escape Sleep 1200ms

Type "q" Sleep 700ms
Type "clear" Enter
Sleep 400ms
