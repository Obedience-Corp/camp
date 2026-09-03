# camp machine — OpenSSH answered but rejected the configured login/key.
#
# The ssh command is a fixture stub. The real branch binary classifies the
# failure, renders the machine TUI, and lets `p` establish the first trusted
# key with one password prompt before continuing through pairing and retesting.
#
# Record (repo root, truecolor):
#   CAMP_VHS_ROOT=$FIXTURE just vhs record-color docs/demos/machine-auth-denied.tape
Output out/evidence/machine-auth-denied/raw.gif

Set Shell "bash"
Set FontFamily "JetBrainsMono Nerd Font"
Set FontSize 15
Set Width 1000
Set Height 640
Set Padding 12
Set Margin 0
Set BorderRadius 0
Set Framerate 30
Set TypingSpeed 45ms
Set Theme {"name":"termcast","background":"#0d0d11","foreground":"#f8f8f2","cursor":"#F2721C","selection":"#2a2a33","black":"#16161a","red":"#ff5f57","green":"#50fa7b","yellow":"#febc2e","blue":"#57a6ff","magenta":"#bd93f9","cyan":"#8be9fd","white":"#f8f8f2","brightBlack":"#6b7280","brightRed":"#ff6e67","brightGreen":"#69ff94","brightYellow":"#ffd23f","brightBlue":"#7ab8ff","brightMagenta":"#d0a9ff","brightCyan":"#a4f0ff","brightWhite":"#ffffff"}

Hide
Type "export HOME=$CAMP_VHS_ROOT/home" Enter
Type "export PATH=$CAMP_VHS_ROOT/bin:$PATH" Enter
Type "export TERM=xterm-256color COLORTERM=truecolor COLORFGBG=15\;0" Enter
Type "export FORCE_COLOR=1 CLICOLOR_FORCE=1" Enter
Type "unset NO_COLOR CLICOLOR" Enter
Type "export CAMP_MACHINES_PATH=$HOME/.obey/machines.yaml" Enter
Type "export CAMP_VHS_ROOT=$CAMP_VHS_ROOT" Enter
Type "export PS1='\033[38;5;208m❯\033[0m '" Enter
Type "cd $CAMP_VHS_ROOT" Enter
Type "clear" Enter
Show

Type "camp machine" Sleep 350ms Enter
Sleep 3000ms
Type "j" Sleep 1200ms
Type "t" Sleep 2500ms
Sleep 3500ms

Type "p" Sleep 3000ms
Enter Sleep 2500ms
Type "camp-demo-password" Sleep 500ms Enter
Sleep 3500ms
Type "y" Sleep 500ms Enter
Sleep 6500ms
