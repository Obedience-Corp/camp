# camp machine TUI — full-color Bubble Tea fleet manager (fixture tailnet only).
#
# Delivery target is a compact colored GIF (see README): record truecolor, then
# scale/optimize. Never record with a live tailscale on PATH.
#
# Setup at $CAMP_VHS_ROOT: bin/camp, bin/tailscale (docs/demos/fixtures/tailscale),
# empty machines file via CAMP_MACHINES_PATH.
#
# Record (repo root, strip NO_COLOR):
#   env -u NO_COLOR -u CLICOLOR TERM=xterm-256color COLORTERM=truecolor \
#     FORCE_COLOR=1 CLICOLOR_FORCE=1 CAMP_VHS_ROOT=$FIXTURE \
#     vhs "$(pwd)/docs/demos/machine-tui.tape"
Output out/machine-tui-raw.gif

Set Shell "bash"
Set FontFamily "JetBrainsMono Nerd Font"
Set FontSize 15
# Match the prior delivery aspect (~960×~600) so the TUI fills the frame
# instead of a tiny form floating in a huge black void.
Set Width 1000
Set Height 640
Set Padding 12
Set Margin 0
Set BorderRadius 0
Set Framerate 24
Set TypingSpeed 50ms
Set Theme {"name":"termcast","background":"#0d0d11","foreground":"#f8f8f2","cursor":"#F2721C","selection":"#2a2a33","black":"#16161a","red":"#ff5f57","green":"#50fa7b","yellow":"#febc2e","blue":"#57a6ff","magenta":"#bd93f9","cyan":"#8be9fd","white":"#f8f8f2","brightBlack":"#6b7280","brightRed":"#ff6e67","brightGreen":"#69ff94","brightYellow":"#ffd23f","brightBlue":"#7ab8ff","brightMagenta":"#d0a9ff","brightCyan":"#a4f0ff","brightWhite":"#ffffff"}

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
Type "clear" Enter
Show

# ── onboarding: discover list fills the screen ──
Type "camp machine" Sleep 400ms Enter
Sleep 3800ms

# highlight a device and open the add form (OpenSSH default)
Type "j" Sleep 900ms
Type "j" Sleep 1100ms
Enter Sleep 2200ms

# form: show OpenSSH default, one cycle to Tailscale SSH, back to OpenSSH
# fields: id → label → host → auth → user → identity
Tab Sleep 350ms
Tab Sleep 350ms
Tab Sleep 700ms
# auth field focused — show Tailscale SSH then return to OpenSSH (do not leave password)
Right Sleep 1600ms
Left Sleep 1400ms

# set user and save
Tab Sleep 400ms
Type "builder" Sleep 900ms
# Save without hanging on a long unreachable probe: cancel form? No — we want save.
# Host will fail DNS outside a real tailnet; exit the TUI before the error steals the demo.
# Instead: Escape cancel, then re-open fleet after CLI add for a clean dual-auth list view.
Escape Sleep 800ms

# Cancel leaves empty fleet onboarding. Use CLI discover for a saved OpenSSH machine,
# then re-enter TUI so the configured fleet panes show in color.
Type "q" Sleep 600ms
Type "clear" Enter
Sleep 300ms
Type "camp machine add --discover --yes --user builder" Sleep 300ms Enter
Sleep 1600ms
Type "camp machine list" Sleep 250ms Enter
Sleep 1400ms
Type "camp machine diagnose" Sleep 250ms Enter
Sleep 2600ms

# re-open TUI on a configured fleet (two-pane layout, full color)
Type "camp machine" Sleep 400ms Enter
Sleep 3500ms
Type "j" Sleep 1200ms
Type "?" Sleep 2800ms
Escape Sleep 1000ms
Type "q" Sleep 600ms
Type "clear" Enter
Sleep 400ms
