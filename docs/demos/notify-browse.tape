# camp notify — the notice browser.
#
# A notice met on camp status is read, copied, dismissed, and restored in
# place, without copying its id into another command. Dismissing the first
# never-synced root surfaces the next one, because the browser re-runs the
# detectors after every write.
#
# One notice is dismissed off-screen first so the Dismissed list has something
# to restore, and the exit report has both a dismissal and a restore to name.
#
# Fixture + record (repo root, truecolor):
#   FIXTURE=$(mktemp -d)
#   docs/demos/fixtures/notify-fixture.sh "$FIXTURE" ./bin/camp
#   CAMP_VHS_ROOT=$FIXTURE just vhs record-color docs/demos/notify-browse.tape
Output out/notify-browse-raw.gif

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

# ── hidden setup: disposable HOME and camp seeded by the fixture ──
Hide
Type "export HOME=$(cd $CAMP_VHS_ROOT/home && pwd -P)" Enter
Type "export PATH=$CAMP_VHS_ROOT/bin:$PATH" Enter
Type "export CAMP_VHS_HANDOFF_LOG=$CAMP_VHS_ROOT/handoff.log" Enter
Type "export CAMP_MACHINE_NAME=fixture-mac TZ=UTC" Enter
Type "unset NO_COLOR CLICOLOR" Enter
Type "export TERM=xterm-256color COLORTERM=truecolor COLORFGBG=15\;0 CLICOLOR_FORCE=1 FORCE_COLOR=1" Enter
Type "export PS1='\033[38;5;208m❯\033[0m '" Enter
Type "cd $CAMP_VHS_ROOT/camp" Enter
Type "camp notify dismiss dungeon-legacy-layout >/dev/null" Enter
Type "clear" Enter
Show

# Open the browser: live notices, dismissed ones, and the selected notice's fix.
Type "camp notify" Sleep 400ms Enter
Sleep 3000ms

# Move to the never-synced artifact root; the detail pane names its subject.
Type "j" Sleep 2200ms

# Hide and reopen the detail pane.
Enter Sleep 1200ms
Enter Sleep 1800ms

# Copy the fix: only the runnable command reaches the clipboard.
Type "y" Sleep 2800ms

# Dismiss it. The detectors re-run, and the next never-synced root surfaces.
Type "d" Sleep 3200ms

# Down into Dismissed, past the root just dismissed, to the earlier dismissal.
Type "j" Sleep 900ms
Type "j" Sleep 1800ms

# Restore it: it returns to the live list.
Type "r" Sleep 3200ms

# Quit; the exit report names each change and its undo command.
Type "q" Sleep 3500ms
