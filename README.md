# fzt-automate

The at menu, its built-in commands, and program launches. The menu and command
store are described in https://forgejo.romaine.life/romaine-life/at.

Failed menu launches and commands are saved as JSON lines under
`~/.fzt-automate/failures/` on Windows, or `failures/` in the platform's config
directory elsewhere (`FZT_CONFIG_DIR` overrides it). Each record includes the
time, entry name, working directory when known, and error text. Each process
owns two rolling files of at most 1 MiB each; old processes' files expire after
30 days when another process writes its first failure. Records limit error text
to 64 KiB; the existing error displays and program exit codes are unchanged.

The output box stays open when a program cannot start. A program that starts in
a new tmux window and then fails keeps its pane through `remain-on-exit=failed`;
tmux also records its exit in its own error history. The latest quick pick's
full output remains in `last-output.txt` beside the menu cache.
