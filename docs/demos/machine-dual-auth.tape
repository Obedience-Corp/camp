# camp machine dual-auth — CLI proof that discover defaults to OpenSSH and
# that --auth tailscale-ssh is opt-in. Uses the fixture tailscale stub only
# (never a live tailnet).
#
# Setup expected at $CAMP_VHS_ROOT: bin/camp, bin/tailscale (fixture), empty
# machines.yaml via CAMP_MACHINES_PATH.
#
# Record with:
#   CAMP_VHS_ROOT=/path/to/fixture vhs docs/demos/machine-dual-auth.tape
Output out/machine-dual-auth.gif

Set Shell "bash"
Set FontFamily "JetBrainsMono Nerd Font"
Set FontSize 16
Set Width 1200
Set Height 720
Set Padding 16
Set Margin 0
Set BorderRadius 0
Set Framerate 24
Set TypingSpeed 55ms
Set Theme {"name":"termcast","background":"#0d0d11","foreground":"#f8f8f2","cursor":"#F2721C","selection":"#2a2a33","black":"#16161a","red":"#ff5f57","green":"#50fa7b","yellow":"#febc2e","blue":"#57a6ff","magenta":"#bd93f9","cyan":"#8be9fd","white":"#f8f8f2","brightBlack":"#6b7280","brightRed":"#ff6e67","brightGreen":"#69ff94","brightYellow":"#ffd23f","brightBlue":"#7ab8ff","brightMagenta":"#d0a9ff","brightCyan":"#a4f0ff","brightWhite":"#ffffff"}

Hide
Type "export HOME=$CAMP_VHS_ROOT/home" Enter
Type "export PATH=$CAMP_VHS_ROOT/bin:$PATH" Enter
Type "export TERM=xterm-256color COLORTERM=truecolor COLORFGBG=15\;0" Enter
Type "export CAMP_MACHINES_PATH=$CAMP_VHS_ROOT/machines-dual.yaml" Enter
Type "export PS1='\033[38;5;208m❯\033[0m '" Enter
Type "cd $CAMP_VHS_ROOT" Enter
Type ": > $CAMP_MACHINES_PATH" Enter
Type "clear" Enter
Show

# ── discover defaults to OpenSSH (ssh-agent) ──
Type "camp machine add --discover --yes" Sleep 300ms Enter
Sleep 1800ms

Type "camp machine list" Sleep 250ms Enter
Sleep 1600ms

Type "camp machine diagnose" Sleep 250ms Enter
Sleep 2800ms

# ── Tailscale SSH is opt-in via --auth ──
Type "camp machine add --discover workstation --auth tailscale-ssh --user builder" Sleep 300ms Enter
Sleep 1800ms

Type "camp machine list" Sleep 250ms Enter
Sleep 1600ms

Type "camp machine diagnose workstation" Sleep 250ms Enter
Sleep 3200ms

Type "clear" Enter
Sleep 400ms
