# Publish the latest GitHub release's Windows `fzt-automate` (the `at` menu) to
# \\NelsonNAS\tools as `fzt-automate` (Nelson's home machines run it from there;
# see shell-config-profile-home claude\topics\home-tools.md). Cut a release first
# (release.yml).
$ErrorActionPreference = 'Stop'

$repo = 'romaine-life/fzt-automate'
$tag = gh release view -R $repo --json tagName --jq .tagName
if ($LASTEXITCODE -ne 0) { throw "no release on $repo" }
$commit = gh api "repos/$repo/commits/$tag" --jq .sha

$dir = Join-Path ([IO.Path]::GetTempPath()) "fzt-automate-$tag"
Remove-Item $dir -Recurse -Force -ErrorAction SilentlyContinue
gh release download $tag -R $repo -p fzt-automate-windows-amd64.exe -D $dir
if ($LASTEXITCODE -ne 0) { throw "downloading fzt-automate-windows-amd64.exe from $tag failed" }
Rename-Item (Join-Path $dir 'fzt-automate-windows-amd64.exe') 'fzt-automate.exe'

publish-tool -Name fzt-automate -Version $tag -Path (Join-Path $dir 'fzt-automate.exe') `
    -Entry fzt-automate.exe -Repo "https://github.com/$repo" -Commit $commit
