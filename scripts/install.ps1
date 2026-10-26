<#
.SYNOPSIS
    Install a released vmrc binary on Windows.

.DESCRIPTION
    Downloads the matching archive from this repo's GitHub Releases, verifies it
    against the release checksums.txt (SHA-256), unpacks it, and places the binary
    in a directory on your PATH. Defaults to installing the agent (vmrc-agent),
    which is the common Windows target; pass -Binary vmrc for the host CLI.

    After the first install the binary updates itself (`vmrc-agent update`).

.EXAMPLE
    irm https://raw.githubusercontent.com/pddstudio/vmrc/main/scripts/install.ps1 | iex

.EXAMPLE
    # Install the host CLI instead of the agent:
    & ([scriptblock]::Create((irm https://raw.githubusercontent.com/pddstudio/vmrc/main/scripts/install.ps1))) -Binary vmrc
#>
[CmdletBinding()]
param(
    [ValidateSet('vmrc', 'vmrc-agent')]
    [string]$Binary = 'vmrc-agent',
    [string]$Version = '',
    [string]$BinDir = '',
    [string]$GitHubToken = $env:GITHUB_TOKEN
)

$ErrorActionPreference = 'Stop'
$Repo = 'pddstudio/vmrc'

# --- detect arch (goreleaser GOARCH names) -----------------------------------
$arch = switch ($env:PROCESSOR_ARCHITECTURE) {
    'AMD64' { 'amd64' }
    'ARM64' { 'arm64' }
    default { throw "Unsupported architecture '$($env:PROCESSOR_ARCHITECTURE)'" }
}
$os = 'windows'

# --- default install dir ------------------------------------------------------
function Test-Admin {
    $id = [Security.Principal.WindowsIdentity]::GetCurrent()
    (New-Object Security.Principal.WindowsPrincipal($id)).IsInRole(
        [Security.Principal.WindowsBuiltInRole]::Administrator)
}
if (-not $BinDir) {
    if (Test-Admin) {
        $BinDir = Join-Path $env:ProgramFiles 'vmrc'
    } else {
        $BinDir = Join-Path $env:LOCALAPPDATA 'Programs\vmrc'
    }
}

# --- http headers -------------------------------------------------------------
$headers = @{ 'User-Agent' = 'vmrc-install' }
if ($GitHubToken) { $headers['Authorization'] = "Bearer $GitHubToken" }

# --- resolve the release ------------------------------------------------------
if ($Version) {
    $api = "https://api.github.com/repos/$Repo/releases/tags/$Version"
} else {
    $api = "https://api.github.com/repos/$Repo/releases/latest"
}

Write-Host "Querying $Repo releases ..."
$release = Invoke-RestMethod -Uri $api -Headers $headers

# Match <binary>_<ver>_windows_<arch>.zip exactly. The '_' separator means 'vmrc'
# won't match a 'vmrc-agent_...' asset and vice versa.
$pattern = "^$([regex]::Escape($Binary))_.*_${os}_${arch}\.zip$"
$asset = $release.assets | Where-Object { $_.name -match $pattern } | Select-Object -First 1
$checksums = $release.assets | Where-Object { $_.name -eq 'checksums.txt' } | Select-Object -First 1

if (-not $asset) { throw "No $Binary asset for $os/$arch in release $($release.tag_name)" }
if (-not $checksums) { throw "Release has no checksums.txt; refusing to install unverified" }

# --- download + verify --------------------------------------------------------
$tmp = Join-Path ([IO.Path]::GetTempPath()) ("vmrc-" + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $tmp | Out-Null
try {
    $zip = Join-Path $tmp $asset.name
    $sumFile = Join-Path $tmp 'checksums.txt'
    Write-Host "Downloading $($asset.name) ..."
    Invoke-WebRequest -Uri $asset.browser_download_url -Headers $headers -OutFile $zip
    Invoke-WebRequest -Uri $checksums.browser_download_url -Headers $headers -OutFile $sumFile

    $want = (Get-Content $sumFile | Where-Object { $_ -match "\s$([regex]::Escape($asset.name))$" } |
        Select-Object -First 1) -split '\s+' | Select-Object -First 1
    if (-not $want) { throw "$($asset.name) not listed in checksums.txt" }
    $got = (Get-FileHash -Algorithm SHA256 -Path $zip).Hash.ToLower()
    if ($want.ToLower() -ne $got) {
        throw "Checksum mismatch for $($asset.name)`n  expected $want`n  got      $got"
    }
    Write-Host "Checksum OK."

    # --- extract + install ----------------------------------------------------
    $extract = Join-Path $tmp 'x'
    Expand-Archive -Path $zip -DestinationPath $extract -Force
    $exe = Join-Path $extract "$Binary.exe"
    if (-not (Test-Path $exe)) { throw "$Binary.exe not found inside the archive" }

    New-Item -ItemType Directory -Path $BinDir -Force | Out-Null
    Copy-Item -Path $exe -Destination (Join-Path $BinDir "$Binary.exe") -Force
    Write-Host ""
    Write-Host "$Binary installed to $(Join-Path $BinDir "$Binary.exe")"
    & (Join-Path $BinDir "$Binary.exe") --version
} finally {
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}

# --- PATH hint ----------------------------------------------------------------
$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if (($userPath -split ';') -notcontains $BinDir -and ($env:Path -split ';') -notcontains $BinDir) {
    Write-Host ""
    Write-Host "note: $BinDir is not on your PATH. Add it for your user with:"
    Write-Host "  [Environment]::SetEnvironmentVariable('Path', `"`$([Environment]::GetEnvironmentVariable('Path','User'));$BinDir`", 'User')"
    Write-Host "  (then open a new terminal)"
}

# --- agent next-step ----------------------------------------------------------
if ($Binary -eq 'vmrc-agent') {
    Write-Host ""
    Write-Host "Next: enroll this VM on your tailnet and install the boot service"
    Write-Host "(run from an Administrator PowerShell):"
    Write-Host "  `$env:TS_AUTHKEY='tskey-...'; vmrc-agent setup --allow-login you@example.com"
}
