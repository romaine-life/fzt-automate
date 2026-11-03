# Publish the latest Forgejo release's Windows `fzt-automate` (the `at` menu) to
# \\NelsonNAS\tools as `fzt-automate` (Nelson's home machines run it from there;
# see shell-config-profile-home claude\topics\home-tools.md). Cut a release first
# (release.yml on Forgejo).
$ErrorActionPreference = 'Stop'

$repo = 'romaine-life/fzt-automate'
$web = "https://forgejo.romaine.life/$repo"
$api = "https://forgejo.romaine.life/api/v1/repos/$repo"
$cred = "protocol=https`nhost=forgejo.romaine.life`n`n" | git credential fill
$token = ($cred | Where-Object { $_ -like 'password=*' }) -replace '^password=', ''
if (-not $token) { throw 'git credential fill returned no token for forgejo.romaine.life' }
$h = @{ Authorization = "token $token" }

$rel = Invoke-RestMethod "$api/releases/latest" -Headers $h
$tag = $rel.tag_name
$commit = (Invoke-RestMethod "$api/tags/$tag" -Headers $h).commit.sha
$asset = $rel.assets | Where-Object name -eq 'fzt-automate-windows-amd64.exe'
if (-not $asset) { throw "$tag has no fzt-automate-windows-amd64.exe yet; is its build still running? $web/actions" }

$dir = Join-Path ([IO.Path]::GetTempPath()) "fzt-automate-$tag"
Remove-Item $dir -Recurse -Force -ErrorAction SilentlyContinue
New-Item -ItemType Directory $dir | Out-Null
$exe = Join-Path $dir 'fzt-automate.exe'
Invoke-WebRequest $asset.browser_download_url -Headers $h -OutFile $exe

publish-tool -Name fzt-automate -Version $tag -Path $exe `
    -Entry fzt-automate.exe -Repo $web -Commit $commit
