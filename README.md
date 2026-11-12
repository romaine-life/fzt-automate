# fzt-automate

The at menu, its built-in commands, and program launches. The menu and command
store are described in https://forgejo.romaine.life/romaine-life/at.

CI, signed builds, and releases run on
[Forgejo](https://forgejo.romaine.life/romaine-life/fzt-automate/actions).
Run `release.yml` on `main` to cut the next `v0.1.N` and build the Windows,
Linux, and macOS binaries. The macOS binary is cross-compiled Go; no Mac
runner is needed. `dispatch.yml` takes fzt and fzt-terminal up through a
checked pull request; strip-owner starts it when it delivers a change to them,
and starts `release.yml` once the take-up merges (PIPT-3).

After the Forgejo release has all three assets, CI starts home-sync on every
PC. It verifies the signed Windows binary and switches the managed installation
at `C:\Program Files\at-home\fzt-automate\current`. Existing terminals use that
stable path immediately. The manual `publish.ps1` was retired by HOMECFG-110.

On Windows a `tool:<name>` action runs a tool home-sync installs:
`C:\Program Files\at-home\<name>\current\<entry>`, where `<entry>` is the
program home-sync's own list (`~/.home-sync/tools.json`) names, copied from
the home profile's `machine/state.json`; `tool:<name>/<file>` is another file
in that folder. So a new app needs only its `state.json` entry to launch from
the menu, and this repo keeps no list of tools: a test launches one its code
has never heard of (HOMECFG-213). A tool missing from the list or from its
folder reports an error; it never launches an old NAS or Drive copy.

The menu has no edit or save commands. It changes only through
fzt-frontend's API, where a Claude session edits it: a save commits the menu
and starts home-sync on every PC.

A desktop program, one with its own window, opens with `launch <name> <program>
[arguments...]`: it switches to the program's window if one is open, else starts
it and returns at once, so it holds no tab and the menu isn't left waiting. The
program is a `tool:` word, a path or a link (`steam://...`); a window matches by
its program's path (through a tool's `current` folder link too), else by its
title or its program's own description being `<name>`. Power Lines is
`launch 'Power Lines' tool:mgba --script tool:power-lines/strings.lua -C
savegamePath=power-lines tool:power-lines/power-lines.gba`. Windows' installed-apps
list plays no part, except in the `programs` folder, which lists that list; its
rows open through `installed-app`.

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

An action starting `system:<name>` is a system app: one window in the `system`
session, not a new tab per pick (TMUX-167). The pick goes to the window marked
`@<name>-window` wherever it is, moving it into `system` and starting its program
again if it has stopped; with none, it makes the window (at-tmux `new-window -M`).
The window stays when the program ends, saying so, and Enter starts it again
(`remain-on-exit relaunch`). A missing `system` session is made first by the home
config's `system-session`. A system app runs a program, not a built-in or an at
command still on PowerShell.

`launch-opencode` picks a repo (below) and replaces its tmux pane with
OpenCode, using the Anthropic API login in `~/.local/share/opencode/auth.json`
(`XDG_DATA_HOME` overrides the data directory). Home-sync provisions the current
`@opencode/cli` npm package, login and Claude model. On Windows the built-in starts
the package's executable directly, bypassing npm's command shim. Missing installs
or API logins report an error before the picker; a failed launch keeps its tab.
`launch-claude` and its subscription billing remain the default.

The Claude launchers start the app tmux's `@claude-app` names: `at-claude`, the
terminal harness, or `claude-code`, Claude Code itself (ATC-6, ATC-7); the home
profile's `tmux/windows.conf` sets it. It is read at each launch, so a change there
takes effect at the next one; an unset or unknown value stops the launch and says so. `launch-claude` picks a
repo (below) and replaces its tmux pane with that app. `resume-claude` picks a past session
by its name and resumes it in its folder; `resume-claude <session id> [--chrome]`
resumes that one in this pane's folder, with Claude in Chrome connected when asked,
for tmux's session restore, reopen-window and `claude-relaunch-with-chrome`.

`work-on-app claude|codex` opens a new tab running that agent in the repo of the app
in the tab you came from, so you don't need to remember which repo holds it. It
reads what the pane runs (below its own process first, so an app the at menu
started isn't taken for fzt-automate), then `machine/state.json`: `tools` for what home-sync
installs, `appRepos` for the rest. A repo this PC hasn't cloned is cloned to
`D:\repos`; a tab with no recognized app gets a folder picker that says so.

`launch-codex` picks a repo (below) and starts the native Codex executable
in the window, using the user's existing Codex login. `resume-codex` opens Codex's
session picker across all folders. Both retain failed launches for inspection.
Both use `--no-daemon` so Codex's lifecycle hooks inherit the launching tmux
pane's environment and can register the session and generate its window name.
Already-running sessions retain their existing runtime; inspect and repair any
missing pane registration separately when changing the launcher.
The popup menu uses Codex's reported folder for actions launched over its window.

The three launchers pick a repo from Forgejo, so every PC offers the same ones
(FZTA-17). The list is the repos starred on Forgejo (star one on its page to pin
it), then the eight this PC's Claude and interactive Codex sessions ran in last
(30 days, worktrees included), then the rest by latest change; mirrors and
archived repos are left out. A pick opens this PC's checkout, found by its origin
remote in `D:\repos` or the shell-config and profile checkouts home-sync keeps,
else clones into `D:\repos`. Two fixed rows start in `D:\repos` itself or open
the folder browser; a launcher given a folder (`launch-claude <folder>`) opens
the browser there, as before. Forgejo is read with the token git holds for it,
never prompting; when it can't be read, the title says why, the full error goes
to the failure log, and the list is this PC's checkouts.
