# camp machine — the fleet manager, recorded against the real binary with a
# stubbed tailnet and an empty machines.yaml so the recording starts where a
# first-time user does.
#
# docs/demos/fixtures/tailscale shadows the real binary on PATH. Recording
# against a real tailnet would bake the recorder's tailnet name, device
# inventory and online state into a GIF in a public repo. Never record this
# tape with the real tailscale ahead of the fixture on PATH.
#
# Setup expected at $CAMP_VHS_ROOT: bin/camp (the branch build), bin/tailscale
# (a copy of the fixture), and no machines.yaml.
#
# Record with: just vhs record "$PWD/docs/demos/machine-tui.tape"
Output out/machine-tui.gif

Set Shell "bash"
Set FontFamily "JetBrainsMono Nerd Font"
Set FontSize 16
Set Width 1360
Set Height 840
Set Padding 20
Set Margin 0
Set BorderRadius 0
Set Framerate 30
Set TypingSpeed 70ms
Set Theme {"name":"termcast","background":"#0d0d11","foreground":"#f8f8f2","cursor":"#F2721C","selection":"#2a2a33","black":"#16161a","red":"#ff5f57","green":"#50fa7b","yellow":"#febc2e","blue":"#57a6ff","magenta":"#bd93f9","cyan":"#8be9fd","white":"#f8f8f2","brightBlack":"#6b7280","brightRed":"#ff6e67","brightGreen":"#69ff94","brightYellow":"#ffd23f","brightBlue":"#7ab8ff","brightMagenta":"#d0a9ff","brightCyan":"#a4f0ff","brightWhite":"#ffffff"}

Hide
Type "export HOME=$CAMP_VHS_ROOT/home" Enter
Type "export PATH=$CAMP_VHS_ROOT/bin:$PATH" Enter
Type "export TERM=xterm-256color COLORTERM=truecolor COLORFGBG=15\;0" Enter
Type "export CAMP_MACHINES_PATH=$CAMP_VHS_ROOT/machines.yaml" Enter
Type "export PS1='\033[38;5;208m❯\033[0m '" Enter
Type "cd $CAMP_VHS_ROOT" Enter
Type "clear" Enter
Show

# ── a first run, with nothing configured ──
Type "camp machine" Sleep 500ms Enter
Sleep 4000ms

# the screen says what a machine is for, and because tailscale is installed it
# already lists the ones this user could add
Type "j" Sleep 1200ms
Type "j" Sleep 1400ms
Enter Sleep 2000ms

# the pick fills the form in; the ssh user is still yours to set
Tab Sleep 300ms
Tab Sleep 300ms
Tab Sleep 300ms
Tab Sleep 600ms
Type "builder" Sleep 1000ms
Enter
Sleep 3000ms

# saving tests the connection straight away, so you learn whether it works
# without going looking for another key
Sleep 3000ms

# ── the fleet, once there is one ──
Type "?" Sleep 3800ms
Escape Sleep 1200ms

Type "q" Sleep 800ms
Type "clear" Enter
Type "camp machine list" Sleep 400ms Enter
Sleep 2600ms
