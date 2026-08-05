# camp triage review — quit mid-lane, reopen, resume
#
# Part of the triage review harness (spec doc 07). Record with:
#   just tui tape docs/demos/triage-review-quit-resume.tape
#
# The fixture script builds a deterministic campaign under a throwaway HOME:
# two parked rows, one terminal row, and one consolidation whose declared
# successor does not exist.

Output docs/demos/triage-review-quit-resume.gif

Set Shell "bash"
Set FontSize 15
Set Width 1300
Set Height 900
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

Enter
Sleep 1800ms
Enter
Sleep 2s

# Approve one row, then quit mid-lane.
Enter
Sleep 2s
Type "q"
Sleep 1500ms

# The verdict is on disk, not in memory.
Type "camp triage status --json | jq -c .run.counts" Sleep 300ms Enter
Sleep 2500ms

# Reopening resumes at the row still undecided.
Type "camp triage review" Sleep 300ms Enter
Sleep 6s
Enter
Sleep 3s

Type "q"
Sleep 1500ms
