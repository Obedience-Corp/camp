# camp worktrees list — project-aware listing and explicit filtering.
#
# The fixture is created in the hidden setup so the recording is reproducible
# and contains no customer data or machine-specific project paths.
# Record with: just vhs record-color docs/demos/worktree-list.tape
# (or unset NO_COLOR/CLICOLOR and enable truecolor before running vhs).
Output out/worktree-list.gif

Set Shell "bash"
Set FontFamily "JetBrainsMono Nerd Font"
Set FontSize 16
Set Width 1360
Set Height 760
Set Padding 20
Set Margin 0
Set BorderRadius 0
Set Framerate 30
Set TypingSpeed 55ms
Set Theme {"name":"termcast","background":"#0d0d11","foreground":"#f8f8f2","cursor":"#F2721C","selection":"#2a2a33","black":"#16161a","red":"#ff5f57","green":"#50fa7b","yellow":"#febc2e","blue":"#57a6ff","magenta":"#bd93f9","cyan":"#8be9fd","white":"#f8f8f2","brightBlack":"#6b7280","brightRed":"#ff6e67","brightGreen":"#69ff94","brightYellow":"#ffd23f","brightBlue":"#7ab8ff","brightMagenta":"#d0a9ff","brightCyan":"#a4f0ff","brightWhite":"#ffffff"}

# ── hidden setup: a disposable campaign with two projects and one worktree each ──
Hide
Type "export HOME=$CAMP_VHS_ROOT/home" Enter
Type "export PATH=$CAMP_VHS_ROOT/bin:$PATH" Enter
Type "unset NO_COLOR CLICOLOR" Enter
Type "export TERM=xterm-256color COLORTERM=truecolor COLORFGBG=15\;0 CLICOLOR_FORCE=1 FORCE_COLOR=1" Enter
Type "export PS1='\033[38;5;208m❯\033[0m '" Enter
Type "camp shell-init bash > $CAMP_VHS_ROOT/shell-init.bash && source $CAMP_VHS_ROOT/shell-init.bash" Enter
Type "camp init $CAMP_VHS_ROOT/campaign --name worktree-list-demo --description 'Project-aware worktree listing' --mission 'Keep worktrees easy to find' --no-register --no-skills" Enter
Type "mkdir -p $CAMP_VHS_ROOT/campaign/projects/camp $CAMP_VHS_ROOT/campaign/projects/other $CAMP_VHS_ROOT/campaign/projects/worktrees/camp $CAMP_VHS_ROOT/campaign/projects/worktrees/other" Enter
Type "git -C $CAMP_VHS_ROOT/campaign/projects/camp init -q && git -C $CAMP_VHS_ROOT/campaign/projects/camp config user.email demo@example.com && git -C $CAMP_VHS_ROOT/campaign/projects/camp config user.name Demo && git -C $CAMP_VHS_ROOT/campaign/projects/camp commit --allow-empty -qm initial" Enter
Type "git -C $CAMP_VHS_ROOT/campaign/projects/other init -q && git -C $CAMP_VHS_ROOT/campaign/projects/other config user.email demo@example.com && git -C $CAMP_VHS_ROOT/campaign/projects/other config user.name Demo && git -C $CAMP_VHS_ROOT/campaign/projects/other commit --allow-empty -qm initial" Enter
Type "git -C $CAMP_VHS_ROOT/campaign/projects/camp worktree add $CAMP_VHS_ROOT/campaign/projects/worktrees/camp/camp-fix -b camp-fix" Enter
Type "git -C $CAMP_VHS_ROOT/campaign/projects/other worktree add $CAMP_VHS_ROOT/campaign/projects/worktrees/other/other-fix -b other-fix" Enter
Type "cd $CAMP_VHS_ROOT/campaign" Enter
Type "clear" Enter
Show

# From the campaign root, the full TUI shows both projects.
Type "camp worktrees list" Sleep 500ms Enter
Sleep 2200ms
Type "j" Sleep 700ms
Type "k" Sleep 700ms
Type "q"
Sleep 1000ms

# cgo navigates into camp; automatic context detection narrows the full TUI.
Type "cgo p camp" Sleep 500ms Enter
Sleep 1200ms
Type "camp worktrees list" Sleep 500ms Enter
Sleep 2200ms
Type "j" Sleep 700ms
Type "q"
Sleep 1000ms

# cgo toggles back to the campaign root; --project filters the full TUI.
Type "cgo" Sleep 500ms Enter
Sleep 1200ms
Type "camp worktrees list --project other --interactive" Sleep 500ms Enter
Sleep 2200ms
Type "j" Sleep 700ms
Type "q"
Sleep 1000ms
