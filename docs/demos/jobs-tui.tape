# camp jobs — interactive deferred-commit queue browser.
#
# Shows the bordered TUI with CREATED timestamps and in-place actions
# (drop/retry) so a user never has to copy a job id out of the listing.
#
# Fixture + record (repo root, truecolor):
#   FIXTURE=$(mktemp -d)
#   docs/demos/fixtures/jobs-tui-fixture.sh "$FIXTURE" ./bin/camp
#   CAMP_VHS_ROOT=$FIXTURE just vhs record-color docs/demos/jobs-tui.tape
Output out/jobs-tui.gif

Set Shell "bash"
Set FontFamily "JetBrainsMono Nerd Font"
Set FontSize 16
Set Width 1200
Set Height 700
Set Padding 18
Set Margin 0
Set BorderRadius 0
Set Framerate 30
Set TypingSpeed 55ms
Set Theme {"name":"termcast","background":"#0d0d11","foreground":"#f8f8f2","cursor":"#F2721C","selection":"#2a2a33","black":"#16161a","red":"#ff5f57","green":"#50fa7b","yellow":"#febc2e","blue":"#57a6ff","magenta":"#bd93f9","cyan":"#8be9fd","white":"#f8f8f2","brightBlack":"#6b7280","brightRed":"#ff6e67","brightGreen":"#69ff94","brightYellow":"#ffd23f","brightBlue":"#7ab8ff","brightMagenta":"#d0a9ff","brightCyan":"#a4f0ff","brightWhite":"#ffffff"}

# ── hidden setup: disposable HOME campaign already seeded by the fixture ──
Hide
Type "export HOME=$(cd $CAMP_VHS_ROOT/home && pwd -P)" Enter
Type "export PATH=$CAMP_VHS_ROOT/bin:$PATH" Enter
Type "unset NO_COLOR CLICOLOR" Enter
Type "export TERM=xterm-256color COLORTERM=truecolor COLORFGBG=15\;0 CLICOLOR_FORCE=1 FORCE_COLOR=1" Enter
Type "export PS1='\033[38;5;208m❯\033[0m '" Enter
Type "cd $HOME/campaign" Enter
Type "clear" Enter
Show

# Open the jobs browser: borders, CREATED column, failed rows, action help.
Type "camp jobs" Sleep 400ms Enter
Sleep 2800ms

# Move to the second failed job so the selection (and its CREATED time) is obvious.
Type "j" Sleep 1200ms

# Drop without copying an id: confirm overlay, then cancel so the queue stays
# for the rest of the demo.
Type "d" Sleep 2200ms
Type "n" Sleep 1000ms

# Retry-all confirm is the other bulk action that used to require a second command.
Type "R" Sleep 2000ms
Type "n" Sleep 1000ms

# Leave the browser on the actionable listing.
Sleep 1800ms
Type "q" Sleep 700ms
Type "clear" Enter
Sleep 400ms
