# camp triage review — amend through the destination picker
#
# Part of the triage review harness (spec doc 07). Record with:
#   just tui tape docs/demos/triage-review-amend.tape
#
# The fixture script builds a deterministic campaign under a throwaway HOME:
# two parked rows, one terminal row, and one consolidation whose declared
# successor does not exist.

Output docs/demos/triage-review-amend.gif

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
Sleep 2s
Enter
Sleep 2500ms

# [m] amend opens the shared picker over the row's type vocabulary.
Down Sleep 400ms
Down Sleep 600ms
Enter
Sleep 2500ms

# Each label shows the action it resolves to.
Down Sleep 500ms
Down Sleep 800ms
Enter
Sleep 2500ms

Type "q"
Sleep 1500ms
