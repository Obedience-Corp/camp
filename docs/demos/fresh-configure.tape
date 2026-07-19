# camp fresh configure — the interactive follow-up setup, recorded against the
# real binary on a fixture campaign with 15 linked projects.
#
# Record with: CAMP_VHS_ROOT=/path/to/fresh-configure-fixture just vhs record docs/demos/fresh-configure.tape
Output out/fresh-configure.gif

Set Shell "bash"
Set FontFamily "JetBrainsMono Nerd Font"
Set FontSize 16
Set Width 1360
Set Height 800
Set Padding 20
Set Margin 0
Set BorderRadius 0
Set Framerate 30
Set TypingSpeed 70ms
Set Theme {"name":"termcast","background":"#0d0d11","foreground":"#f8f8f2","cursor":"#F2721C","selection":"#2a2a33","black":"#16161a","red":"#ff5f57","green":"#50fa7b","yellow":"#febc2e","blue":"#57a6ff","magenta":"#bd93f9","cyan":"#8be9fd","white":"#f8f8f2","brightBlack":"#6b7280","brightRed":"#ff6e67","brightGreen":"#69ff94","brightYellow":"#ffd23f","brightBlue":"#7ab8ff","brightMagenta":"#d0a9ff","brightCyan":"#a4f0ff","brightWhite":"#ffffff"}

# ── hidden setup: branch binary on PATH, standing inside a project ──
Hide
Type "export HOME=$CAMP_VHS_ROOT/home" Enter
Type "export PATH=$CAMP_VHS_ROOT/bin:$PATH" Enter
Type "export TERM=xterm-256color COLORTERM=truecolor COLORFGBG=15\;0" Enter
Type "export PS1='\033[38;5;208m❯\033[0m '" Enter
Type "cd $CAMP_VHS_ROOT/campaigns/fresh-demo/projects/payments-api" Enter
Type "clear" Enter
Show

# ── the campaign already has global follow-ups every project inherits ──
Type "camp fresh configure show" Sleep 500ms Enter
Sleep 2600ms

# ── open the setup from inside a project: it lands on that project ──
Type "camp fresh configure" Sleep 500ms Enter
Sleep 3200ms

# the sidebar holds 15 projects inside its border, payments-api is marked
# "here", and the workflow pane says the project inherits the global steps
Sleep 2200ms

# ── add a build command that belongs to this project only ──
Type "a" Sleep 1400ms
Type "build" Sleep 900ms
Enter Sleep 600ms
Type "cargo build --release" Sleep 1200ms
Enter Sleep 500ms
Enter Sleep 500ms

# the form states what saving does before it does it
Sleep 1800ms
Enter
Sleep 3000ms

Type "q"
Sleep 900ms

# ── the step is real: fresh resolves it for this project, and only this one ──
Type "clear" Enter
Type "camp fresh show-workflow payments-api | tail -8" Sleep 400ms Enter
Sleep 3200ms
Type "camp fresh show-workflow web-storefront | tail -6" Sleep 400ms Enter
Sleep 3400ms
