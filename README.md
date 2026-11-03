# fzt-automate

The at menu, its built-in commands, and program launches. The menu and command
store are described in https://forgejo.romaine.life/romaine-life/at.

CI, signed builds, and releases run on
[Forgejo](https://forgejo.romaine.life/romaine-life/fzt-automate/actions).
Run `release.yml` on `main` to cut the next `v0.1.N` and build the Windows,
Linux, and macOS binaries. The macOS binary is cross-compiled Go; no Mac
runner is needed. Run `dispatch.yml` to update fzt and fzt-terminal through
a checked pull request before releasing.

After the Forgejo release has all three assets, publish its Windows binary
to the home tools share from PowerShell:

```powershell
.\publish.ps1
```

`publish.ps1` downloads the latest Forgejo release and calls:

```powershell
publish-tool -Name fzt-automate -Version $tag -Path $exe `
    -Entry fzt-automate.exe -Repo $web -Commit $commit
```

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
