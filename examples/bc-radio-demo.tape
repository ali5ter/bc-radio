Output bc-radio-demo.gif

Set FontSize 13
Set Width 1280
Set Height 720
Set Padding 10
Set Shell bash

Hide
Type "cd .. && CGO_ENABLED=1 go build -o bc-radio ./cmd/bc-radio 2>/dev/null" Enter
Sleep 5s
Type `PS1="\[\e[38;5;63m\]❯\[\e[0m\] "` Enter
Type "clear" Enter
Sleep 200ms
Show

# Launch with the adaptive theme at full volume (real audio plays while vhs
# renders); the page cache is warm, so the index builds from disk.
Type "./bc-radio -username ali5ter"
Sleep 600ms
Enter

# Library build: the status line reads "building track index…" until the walk
# over the cached discographies lands (10–22 s warm).
Sleep 2s

# Space starts the radio: a random index track, the bar spectrum dancing.
Type " "
Sleep 2s

# Cycle the visualizers: spectrum → particles → radial → scope.
Type "v"
Sleep 2s
Type "v"
Sleep 2s
Type "v"
Sleep 2s

# Fullscreen visualizer, then back.
Type "f"
Sleep 2s
Type "f"
Sleep 1s

# Library: an artist's releases, queue an album (up next shows the queue),
# back to the player.
Type "l"
Sleep 1s
Down
Sleep 400ms
Down
Sleep 400ms
Right
Sleep 500ms
Down
Sleep 500ms
Enter
Sleep 2s
Escape
Sleep 2s

# Help overlay, then out.
Type "?"
Sleep 2s
Type "?"
Sleep 1s
# q quits (this vhs build types "ctrl+c" as literal characters, which would
# hit the repeat and library keys on the way through).
Type "q"
Sleep 1s
