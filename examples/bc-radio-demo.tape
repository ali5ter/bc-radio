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
# renders). The page cache was refreshed before the render, so the index
# walk reads fresh pages from disk and lands in a few seconds on a quiet
# machine — and when it lands with the engine idle, the radio starts by
# itself: no keypress, the bar spectrum dancing on a random track.
Type "./bc-radio -username ali5ter"
Sleep 600ms
Enter

# Wait for the walk: on a quiet machine a fresh-cache walk lands in a few
# seconds and the radio is already playing by the first space; the margin
# covers a loaded machine, where the build screen (spinner + per-band
# counter) is the content in the meantime.
Sleep 10s

# Space pauses — the bars hold exactly where the pause found them — then
# resumes.
Type " "
Sleep 2s
Type " "
Sleep 1s

# Cycle the visualizers: spectrum → particles → scope.
Type "v"
Sleep 2s
Type "v"
Sleep 2s
Type "v"
Sleep 1s

# Fullscreen visualizer, then back.
Type "f"
Sleep 2s
Type "f"
Sleep 1s

# Radio off: the standing line goes plain and the up-next row goes blank —
# the app stops picking on its own. On again: the radio returns with its
# prepared draw.
Type "d"
Sleep 2s
Type "d"
Sleep 2s

# Library: browse the three columns under the live playback header, queue
# an album, back to the player.
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

# Help overlay: the keybindings over the live screen, then out.
Type "?"
Sleep 2s
Type "?"
Sleep 1s
# q quits: the gradient wordmark banner and the thank-you (this vhs build
# types "ctrl+c" as literal characters, which would hit the repeat and
# library keys on the way through).
Type "q"
Sleep 1s
