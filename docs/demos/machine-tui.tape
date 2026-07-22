# camp machine TUI — fleet manager against the real binary, stubbed tailnet,
# empty machines.yaml (first-time user path). Full truecolor required so the
# Bubble Tea UI and fire palette are visible in the GIF.
#
# docs/demos/fixtures/tailscale shadows the real binary on PATH. Never record
# with a live tailscale ahead of the fixture.
#
# Setup at $CAMP_VHS_ROOT: bin/camp, bin/tailscale (fixture copy), empty home.
#
# Record with full color (from repo root):
#   CAMP_VHS_ROOT=/path/to/fixture just vhs record-color docs/demos/machine-tui.tape
# or:
#   env -u NO_COLOR -u CLICOLOR TERM=xterm-256color COLORTERM=truecolor \
#     FORCE_COLOR=1 CLICOLOR_FORCE=1 vhs docs/demos/machine-tui.tape
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
Set TypingSpeed 65ms
Set Theme {"name":"termcast","background":"#0d0d11","foreground":"#f8f8f2","cursor":"#F2721C","selection":"#2a2a33","black":"#16161a","red":"#ff5f57","green":"#50fa7b","yellow":"#febc2e","blue":"#57a6ff","magenta":"#bd93f9","cyan":"#8be9fd","white":"#f8f8f2","brightBlack":"#6b7280","brightRed":"#ff6e67","brightGreen":"#69ff94","brightYellow":"#ffd23f","brightBlue":"#7ab8ff","brightMagenta":"#d0a9ff","brightCyan":"#a4f0ff","brightWhite":"#ffffff"}

Hide
Type "export HOME=$CAMP_VHS_ROOT/home" Enter
Type "export PATH=$CAMP_VHS_ROOT/bin:$PATH" Enter
Type "export TERM=xterm-256color" Enter
Type "export COLORTERM=truecolor" Enter
Type "export COLORFGBG=15\;0" Enter
Type "export FORCE_COLOR=1 CLICOLOR_FORCE=1" Enter
Type "unset NO_COLOR CLICOLOR" Enter
Type "export CAMP_MACHINES_PATH=$CAMP_VHS_ROOT/machines.yaml" Enter
Type ": > $CAMP_MACHINES_PATH" Enter
Type "export PS1='\033[38;5;208m❯\033[0m '" Enter
Type "cd $CAMP_VHS_ROOT" Enter
Type "clear" Enter
Show

# ── first run: onboarding + discoverable tailnet devices ──
Type "camp machine" Sleep 600ms Enter
Sleep 4500ms

# move through the discovered list and open one (fills form with OpenSSH default)
Type "j" Sleep 1400ms
Type "j" Sleep 1600ms
Enter Sleep 2500ms

# form is open — land on auth field and cycle so OpenSSH vs Tailscale SSH are visible
# field order: id, label, host, auth, user, identity · ←/→ cycles auth
Tab Sleep 400ms
Tab Sleep 400ms
Tab Sleep 900ms
# OpenSSH (default) → Tailscale SSH → password → OpenSSH (save with OpenSSH)
Right Sleep 1400ms
Right Sleep 1400ms
Left Sleep 1200ms
Left Sleep 1000ms

# set ssh user
Tab Sleep 500ms
Type "builder" Sleep 1100ms
Enter
Sleep 3500ms

# connection probe runs; fixture host is unreachable — UI still shows fleet + health
Sleep 3500ms

# help overlay shows actions in the TUI
Type "?" Sleep 4000ms
Escape Sleep 1400ms

# quit TUI, then list + diagnose so auth label and PROBE show in color CLI too
Type "q" Sleep 900ms
Type "clear" Enter
Sleep 400ms
Type "camp machine list" Sleep 400ms Enter
Sleep 2200ms
Type "camp machine diagnose" Sleep 400ms Enter
Sleep 3200ms
