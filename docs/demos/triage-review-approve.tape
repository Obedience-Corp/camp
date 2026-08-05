# camp triage review — summary to lane to row approve
#
# Part of the triage review harness (spec doc 07). Record with:
#   just tui tape docs/demos/triage-review-approve.tape
#
# The fixture script builds a deterministic campaign under a throwaway HOME:
# two parked rows, one terminal row, and one consolidation whose declared
# successor does not exist.

Output docs/demos/triage-review-approve.gif

Set Shell "bash"
Set FontSize 15
Set Width 1300
Set Height 860
Set Padding 16
Set Framerate 24
Set TypingSpeed 40ms

Env "TERM" "xterm-256color"
Env "NO_COLOR" "1"

Hide
Type "export PATH=$PWD/bin:$PATH" Enter
Type "export CAMP_BIN=$PWD/bin/camp" Enter
Type "bash docs/demos/fixtures/triage-review-fixture.sh > /tmp/triage-tape.env" Enter
Sleep 3s
Type "source /tmp/triage-tape.env" Enter
Type "cd $TRIAGE_FIXTURE" Enter
Type "export PS1='$ '" Enter
Type "clear" Enter
Show

Type "camp triage review" Sleep 500ms Enter
Sleep 6s

# Screen 1: the lane table. Terminal lanes are marked.
Sleep 2s

# Enter the first (non-terminal) lane.
Enter
Sleep 2500ms

# Open the first row card: evidence, the exact command, the actions.
Enter
Sleep 3s

# [y] approve.
Enter
Sleep 2500ms

Type "q"
Sleep 1500ms
