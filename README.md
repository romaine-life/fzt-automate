# fzt-automate

The at menu, its built-in commands, and program launches. The menu and command
store are described in https://forgejo.romaine.life/romaine-life/at.

CI, signed builds, and releases run on
[Forgejo](https://forgejo.romaine.life/romaine-life/fzt-automate/actions).
Run `release.yml` on `main` to cut the next `v0.1.N` and build the Windows,
Linux, and macOS binaries. The macOS binary is cross-compiled Go; no Mac
runner is needed. Run `dispatch.yml` to update fzt and fzt-terminal through
a checked pull request before releasing.

After the Forgejo release has all three assets, CI starts home-sync on every
PC. It verifies the signed Windows binary and switches the managed installation
at `C:\Program Files\at-home\fzt-automate\current`. Existing terminals use that
stable path immediately. The manual `publish.ps1` was retired by HOMECFG-110.

The guitar-tabs menu action uses its managed installation at
`C:\Program Files\at-home\guitar-tabs\current\guitar-tabs.exe`. A missing
installation reports an error; it never launches an old NAS or Drive copy.

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

`launch-opencode [start-folder]` picks a folder and replaces its tmux pane with
OpenCode, using the Anthropic API login in `~/.local/share/opencode/auth.json`
(`XDG_DATA_HOME` overrides the data directory). Home-sync provisions the current
`@opencode/cli` npm package, login and Claude model. On Windows the built-in starts
the package's executable directly, bypassing npm's command shim. Missing installs
or API logins report an error before the picker; a failed launch keeps its tab.
`launch-claude` and its subscription billing remain the default.

`work-on-app claude|codex` opens a new tab running that agent in the repo of the app
in the tab you came from, so you don't need to remember which repo holds it. It
reads the pane's process, then `machine/state.json`: `tools` for what home-sync
installs, `appRepos` for the rest. A repo this PC hasn't cloned is cloned to
`D:\repos`; a tab with no recognized app gets a folder picker that says so.

`launch-codex [start-folder]` picks a folder and starts the native Codex executable
in the window, using the user's existing Codex login. `resume-codex` opens Codex's
session picker across all folders. Both retain failed launches for inspection.
Both use `--no-daemon` so Codex's lifecycle hooks inherit the launching tmux
pane's environment and can register the session and generate its window name.
Already-running sessions retain their existing runtime; inspect and repair any
missing pane registration separately when changing the launcher.
The popup menu uses Codex's reported folder for actions launched over its window.
