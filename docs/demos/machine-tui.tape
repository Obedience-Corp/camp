# camp machine TUI — the interactive fleet manager (two-pane Bubble Tea UI).
#
# Seed machines off-screen so the visible recording is almost entirely the TUI,
# not CLI list/diagnose. Fixture tailscale only; never a live tailnet.
#
# Record (repo root, truecolor):
#   env -u NO_COLOR -u CLICOLOR TERM=xterm-256color COLORTERM=truecolor \
#     FORCE_COLOR=1 CLICOLOR_FORCE=1 CAMP_VHS_ROOT=$FIXTURE \
#     vhs "$(pwd)/docs/demos/machine-tui.tape"
Output out/machine-tui-raw.gif

Set Shell "bash"
Set FontFamily "JetBrainsMono Nerd Font"
Set FontSize 15
Set Width 1000
Set Height 640
Set Padding 12
Set Margin 0
Set BorderRadius 0
Set Framerate 24
Set TypingSpeed 45ms
Set Theme {"name":"termcast","background":"#0d0d11","foreground":"#f8f8f2","cursor":"#F2721C","selection":"#2a2a33","black":"#16161a","red":"#ff5f57","green":"#50fa7b","yellow":"#febc2e","blue":"#57a6ff","magenta":"#bd93f9","cyan":"#8be9fd","white":"#f8f8f2","brightBlack":"#6b7280","brightRed":"#ff6e67","brightGreen":"#69ff94","brightYellow":"#ffd23f","brightBlue":"#7ab8ff","brightMagenta":"#d0a9ff","brightCyan":"#a4f0ff","brightWhite":"#ffffff"}

# ── hidden setup: env + seed one OpenSSH machine via discover ──
Hide
Type "export HOME=$CAMP_VHS_ROOT/home" Enter
Type "export PATH=$CAMP_VHS_ROOT/bin:$PATH" Enter
Type "export TERM=xterm-256color COLORTERM=truecolor COLORFGBG=15\;0" Enter
Type "export FORCE_COLOR=1 CLICOLOR_FORCE=1" Enter
Type "unset NO_COLOR CLICOLOR" Enter
Type "export CAMP_MACHINES_PATH=$CAMP_VHS_ROOT/machines.yaml" Enter
Type ": > $CAMP_MACHINES_PATH" Enter
Type "export PS1='\033[38;5;208m❯\033[0m '" Enter
Type "cd $CAMP_VHS_ROOT" Enter
# Seed fleet so camp machine opens the two-pane TUI (not empty onboarding)
Type "camp machine add --discover --yes --user builder" Enter
Sleep 800ms
Type "clear" Enter
Show

# ── THE PRODUCT: camp machine TUI ──
Type "camp machine" Sleep 350ms Enter
Sleep 4000ms

# select the remote machine (local is first; j moves to buildfarm)
Type "j" Sleep 1800ms
Sleep 1500ms

# help overlay (still in TUI)
Type "?" Sleep 3200ms
Escape Sleep 1200ms

# open edit form on the selected machine
# edit form starts on Label (id is fixed/skipped)
Type "e" Sleep 2500ms

# Label → Host → Auth method
Tab Sleep 450ms
Tab Sleep 700ms
# Auth method focused: OpenSSH default → Tailscale SSH → OpenSSH
Right Sleep 2000ms
Left Sleep 1800ms
Escape Sleep 1000ms

# rescan tailnet picker (TUI discover overlay)
Type "s" Sleep 3500ms
Type "j" Sleep 1200ms
Escape Sleep 1000ms

# back on fleet; leave the TUI
Sleep 2000ms
Type "q" Sleep 700ms
Type "clear" Enter
Sleep 500ms
