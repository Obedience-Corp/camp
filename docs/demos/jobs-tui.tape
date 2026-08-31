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
Set Theme {"name":"Obedience Corp","background":"#0D0D11","foreground":"#F8F8F2","selection":"#3A2418","cursor":"#F2721C","black":"#0D0D11","red":"#FF5F57","green":"#50FA7B","yellow":"#FEBC2E","blue":"#F2721C","magenta":"#C2410C","cyan":"#FFB347","white":"#F8F8F2","brightBlack":"#A1A1AA","brightRed":"#FF5F57","brightGreen":"#50FA7B","brightYellow":"#FEBC2E","brightBlue":"#FFB347","brightMagenta":"#EA5513","brightCyan":"#FFB347","brightWhite":"#FFFFFF"}

# ── hidden setup: disposable HOME campaign already seeded by the fixture ──
Hide
Type "export HOME=$(cd $CAMP_VHS_ROOT/home && pwd -P)" Enter
Type "export PATH=$CAMP_VHS_ROOT/bin:$PATH" Enter
Type "unset NO_COLOR CLICOLOR" Enter
Type "export TERM=xterm-256color COLORTERM=truecolor COLORFGBG=15\;0 CLICOLOR_FORCE=1 FORCE_COLOR=1" Enter
Type "export PS1='\033[38;5;208m❯\033[0m '" Enter
# Pin the dark fire palette so adaptive detection cannot mute the TUI in vhs.
Type "camp settings set global.theme dark" Enter
Type "camp settings set local.theme_override dark" Enter
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
