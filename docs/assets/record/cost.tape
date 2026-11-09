# Feature clip: cost per role, per round, per feature
# Recorded by render.sh clips against the state left by tour.tape (pending-review).
Output "/tmp/4xdemo/vhs/cost-raw.mp4"

Require vhs

Set Shell "bash"
Set FontFamily "JetBrains Mono"
Set FontSize 17
Set Width 1200
Set Height 560
Set Padding 24
Set Framerate 20
Set WindowBar Colorful
Set Theme {"name":"4x","black":"#484f58","red":"#ff7b72","green":"#7ee787","yellow":"#e3b341","blue":"#79c0ff","magenta":"#d2a8ff","cyan":"#76e3ea","white":"#b1bac4","brightBlack":"#6e7681","brightRed":"#ffa198","brightGreen":"#56d364","brightYellow":"#e3b341","brightBlue":"#a5d6ff","brightMagenta":"#e2c5ff","brightCyan":"#b3f0ff","brightWhite":"#ffffff","background":"#0d1117","foreground":"#e6edf3","selection":"#264f78","cursor":"#d2a8ff"}
Set TypingSpeed 35ms
Set CursorBlink false

Hide
Type `export BASH_SILENCE_DEPRECATION_WARNING=1 HOME=/tmp/4xdemo/home PATH="/tmp/4xdemo/bin:$PATH"; cd /tmp/4xdemo/work/greet && export PS1='\[\e[35m\]greet $\[\e[0m\] ' && clear`
Enter
Sleep 500ms
Show

Type "4x cost"
Sleep 200ms
Enter
Sleep 3.5s
Type "clear; 4x cost --feature F001-greet-by-name"
Sleep 200ms
Enter
Sleep 4s
