# camp project list — interactive browser: group, filter (including the letter g), jump.
#
# Fixture is created in the hidden setup so the recording is reproducible and
# contains no host paths. Record with:
#   CAMP_VHS_ROOT=/path/to/fixture just vhs record-color docs/demos/project-list.tape
Output out/project-list.gif

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

# ── hidden setup: disposable campaign under HOME so paths render as ~/… ──
Hide
Type "export HOME=$(cd $CAMP_VHS_ROOT/home && pwd -P)" Enter
Type "export PATH=$CAMP_VHS_ROOT/bin:$PATH" Enter
Type "unset NO_COLOR CLICOLOR" Enter
Type "export TERM=xterm-256color COLORTERM=truecolor COLORFGBG=15\;0 CLICOLOR_FORCE=1 FORCE_COLOR=1" Enter
Type "export PS1='\033[38;5;208m❯\033[0m '" Enter
Type "camp shell-init bash > $CAMP_VHS_ROOT/shell-init.bash && source $CAMP_VHS_ROOT/shell-init.bash" Enter
Type "camp init $HOME/campaign --name project-list-demo --description 'Interactive project browser' --mission 'Find a project and jump' --no-register --no-skills" Enter
Type "mkdir -p $HOME/campaign/projects/{atlas,grok-cli,web,tools,notes}" Enter
Type "echo module atlas > $HOME/campaign/projects/atlas/go.mod" Enter
Type "echo module grok-cli > $HOME/campaign/projects/grok-cli/go.mod" Enter
Type "echo '{}' > $HOME/campaign/projects/web/package.json" Enter
Type "echo name=tools > $HOME/campaign/projects/tools/pyproject.toml" Enter
Type "echo notes > $HOME/campaign/projects/notes/README.md" Enter
Type "for p in atlas grok-cli web tools notes; do git -C $HOME/campaign/projects/$p init -q && git -C $HOME/campaign/projects/$p config user.email demo@example.com && git -C $HOME/campaign/projects/$p config user.name Demo && git -C $HOME/campaign/projects/$p add -A && git -C $HOME/campaign/projects/$p commit -qm initial; done" Enter
Type "cd $HOME/campaign" Enter
Type "clear" Enter
Show

# Open the browser: type groups, colored badges, path detail.
Type "camp project list" Sleep 400ms Enter
Sleep 2400ms
Type "j" Sleep 700ms
Type "k" Sleep 700ms

# Filter on "go" — the letter g must type into the query, not jump.
Type "/" Sleep 600ms
Type "go" Sleep 1200ms
Sleep 1600ms
Type "j" Sleep 800ms
Enter
Sleep 1800ms

# Landed in the selected Go project (go.mod is the type marker).
Type "ls" Sleep 400ms Enter
Sleep 2200ms
