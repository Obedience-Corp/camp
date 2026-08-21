# The completed-run prompt, recorded against the real branch binary.
#
# camp fresh's completed_runs now defaults to prompt, and its prompt mode calls
# the same runSweepPrompt this command does, so what the recording shows is what
# a fresh run asks. The tape drives the command directly because the prompt runs
# at the campaign root and the fixture campaign has no project submodules for
# fresh to cycle.
#
# Record with: just vhs record-color docs/demos/sweep-prompt.tape
# Needs the branch binary at bin/camp first: just build stable
# The tape builds its own throwaway campaign from
# docs/demos/fixtures/sweep-prompt-fixture.sh under a fake HOME.
Output out/sweep-prompt.gif

Set Shell "bash"
Set FontFamily "JetBrainsMono Nerd Font"
Set FontSize 16
Set Width 1440
Set Height 800
Set Padding 20
Set Margin 0
Set BorderRadius 0
Set Framerate 30
Set TypingSpeed 70ms
Set Theme {"name":"termcast","background":"#0d0d11","foreground":"#f8f8f2","cursor":"#F2721C","selection":"#2a2a33","black":"#16161a","red":"#ff5f57","green":"#50fa7b","yellow":"#febc2e","blue":"#57a6ff","magenta":"#bd93f9","cyan":"#8be9fd","white":"#f8f8f2","brightBlack":"#6b7280","brightRed":"#ff6e67","brightGreen":"#69ff94","brightYellow":"#ffd23f","brightBlue":"#7ab8ff","brightMagenta":"#d0a9ff","brightCyan":"#a4f0ff","brightWhite":"#ffffff"}

# ── hidden setup: branch binary on PATH, throwaway campaign under a fake HOME ──
Hide
Type "export PATH=$PWD/bin:$PATH" Enter
Type "export TERM=xterm-256color COLORTERM=truecolor COLORFGBG=15\;0" Enter
Type "export PS1='\033[38;5;208m❯\033[0m '" Enter
Type `eval "$(CAMP_BIN=$PWD/bin/camp bash docs/demos/fixtures/sweep-prompt-fixture.sh)"` Enter
Sleep 10s
Type "export HOME=$SWEEP_FIXTURE_HOME" Enter
Type "cd $SWEEP_FIXTURE" Enter
Type "clear" Enter
Show

# ── three workitems; two of them finished a workflow run ──
# --list because a bare `camp wi` opens the dashboard TUI on a terminal.
Type "camp wi --list" Sleep 500ms Enter
Sleep 3s

# ── the banner names the command, so run it ──
Type "camp workitem sweep --prompt" Sleep 500ms Enter
Sleep 3500ms

# the design is reported rather than asked about: a completed run means a
# specification exists, not that anyone built it. the explore item gets the
# routing question, opened on the answer that moves nothing.
Sleep 2s

# ── walk up through every answer to Route to docs ──
Up Sleep 700ms
Up Sleep 700ms
Up Sleep 700ms
Up Sleep 1300ms
Enter Sleep 2500ms

# ── the docs browser, the same picker the dungeon crawl uses ──
Down Sleep 700ms
Down Sleep 1600ms
Enter Sleep 2600ms

# ── the chore is a plain confirm: for a chore the loop was the work ──
Left Sleep 1300ms
Enter Sleep 3s

# ── the disk agrees: only the design is still active, and both moves committed ──
# no clear here on purpose: the report line and both results stay on screen next
# to the state they produced. --no-pager because less would take the screen.
Type "camp wi --list" Sleep 400ms Enter
Sleep 3s
Type "git --no-pager log --oneline -3" Sleep 400ms Enter
Sleep 3500ms
