# camp machine — the interactive fleet manager, recorded against the real
# binary with a fixture machines.yaml and a stubbed tailnet.
#
# Discovery shells out to `tailscale status --json`. Recording that against a
# real tailnet would bake the recorder's own tailnet name, device inventory and
# online state into a GIF in a public repo, and would make the recording depend
# on whichever machines happened to be awake. docs/demos/fixtures/tailscale
# shadows the real binary on PATH and returns an invented tailnet instead.
# Never record this tape with the real tailscale ahead of the fixture on PATH.
#
# Setup expected at /tmp/mdemo: bin/camp (the branch build), bin/tailscale
# (a copy of docs/demos/fixtures/tailscale), and machines.yaml.
#
# Record with: just vhs record "$PWD/docs/demos/machine-tui.tape"
Output out/machine-tui.gif

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

# ── hidden setup: branch binary and the tailnet stub ahead of anything real ──
Hide
Type "export PATH=/tmp/mdemo/bin:$PATH" Enter
Type "export TERM=xterm-256color COLORTERM=truecolor" Enter
Type "export CAMP_MACHINES_PATH=/tmp/mdemo/machines.yaml" Enter
Type "export PS1='\033[38;5;208m❯\033[0m '" Enter
Type "cd /tmp/mdemo" Enter
Type "clear" Enter
Show

# ── the fleet as the CLI shows it today ──
Type "camp machine list" Sleep 500ms Enter
Sleep 2400ms

# ── bare 'camp machine' now opens the fleet manager ──
Type "camp machine" Sleep 500ms Enter
Sleep 2600ms

# walk the fleet: local, then each configured machine with its ssh detail
# and ControlMaster socket state
Type "j" Sleep 1900ms
Type "j" Sleep 2100ms

# ── discover: scan the tailnet and pick a device ──
Type "s" Sleep 2600ms
Type "j" Sleep 900ms
Type "j" Sleep 700ms
Type "j" Sleep 1100ms
Enter Sleep 2200ms

# the pick prefills the form instead of saving it: id, label, host and
# tailscale-ssh come from the device, and the ssh user is still yours to set
# before anything is written
Tab Sleep 400ms
Tab Sleep 400ms
Tab Sleep 400ms
Tab Sleep 700ms
Type "builder" Sleep 1300ms
Enter
Sleep 2600ms

# ── edit an existing machine: the id stays fixed, everything else changes ──
Type "k" Sleep 800ms
Type "k" Sleep 1000ms
Type "e" Sleep 1700ms
Tab Sleep 700ms
Ctrl+U Sleep 600ms
Type "10.0.0.99" Sleep 1200ms
Enter
Sleep 2600ms

# ── quit and confirm the file is what the TUI said it was ──
Type "q" Sleep 800ms
Type "clear" Enter
Type "camp machine list" Sleep 400ms Enter
Sleep 3000ms
