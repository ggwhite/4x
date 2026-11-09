# Hero tour, part 1: one real `4x run` on a tiny Go project, `4x init` to review.
# Part 2 (tour-end.tape) signs it off; render.sh joins them.
# Recorded with: docs/assets/record/render.sh tour   (after setup-demo.sh)
# Everything runs in /tmp/4xdemo (throwaway HOME + project). The run calls the
# real Claude Code runner, so it spends tokens; render.sh speeds up the
# long waits when it cuts the clip.
Output "/tmp/4xdemo/vhs/tour-raw.mp4"

Require vhs

Set Shell "bash"
Set FontFamily "JetBrains Mono"
Set FontSize 17
Set Width 1200
Set Height 700
Set Padding 24
Set Framerate 20
Set WindowBar Colorful
Set Theme {"name":"4x","black":"#484f58","red":"#ff7b72","green":"#7ee787","yellow":"#e3b341","blue":"#79c0ff","magenta":"#d2a8ff","cyan":"#76e3ea","white":"#b1bac4","brightBlack":"#6e7681","brightRed":"#ffa198","brightGreen":"#56d364","brightYellow":"#e3b341","brightBlue":"#a5d6ff","brightMagenta":"#e2c5ff","brightCyan":"#b3f0ff","brightWhite":"#ffffff","background":"#0d1117","foreground":"#e6edf3","selection":"#264f78","cursor":"#d2a8ff"}
Set TypingSpeed 35ms
Set CursorBlink false

Hide
Type `export BASH_SILENCE_DEPRECATION_WARNING=1 R="$PWD" HOME=/tmp/4xdemo/home PATH="/tmp/4xdemo/bin:$PATH"; cd /tmp/4xdemo/work/greet && export PS1='\[\e[35m\]greet $\[\e[0m\] ' && clear`
Enter
Sleep 500ms
Show

Type "# 1. Set up 4x in an existing Go project"
Enter
Type "4x init"
Sleep 200ms
Enter
Sleep 3s

Hide
Type `"$R/docs/assets/record/demo-config.sh" && clear`
Enter
Sleep 1s
Show

Type "# 2. Describe a feature"
Enter
Type@20ms `4x new "Greet by name" --id greet-by-name --profile demo --desc "Add a -name flag: 'greet -name Ada' prints 'hello, Ada' (default 'hello, world'). Put it in greet(name string) with a table-driven test."`
Sleep 300ms
Enter
Sleep 2.5s

Type "# 3. Run the loop: Design -> Code -> Review -> Test -> Accept"
Enter
Type "4x run F001"
Sleep 200ms
Enter
# Wait+Screen only sees the top of the buffer once output scrolls; wait for the prompt instead.
Wait+Line@45m /^greet \$ *$/
Sleep 4s

Type "clear; 4x status"
Sleep 200ms
Enter
Sleep 3s
