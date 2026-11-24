[CmdletBinding()]
param(
	[string]$Version = $env:AVIARY_VERSION,
	[string]$Repo = $(if ($env:AVIARY_REPO) { $env:AVIARY_REPO } else { "lsegal/aviary" }),
	[string]$DownloadBase = $(if ($env:AVIARY_DOWNLOAD_BASE) { $env:AVIARY_DOWNLOAD_BASE } else { "https://github.com" }),
	[switch]$SkipSkills = $($env:AVIARY_SKIP_SKILLS -and $env:AVIARY_SKIP_SKILLS -ne "0")
)

$ErrorActionPreference = "Stop"

function Get-BinDir {
	if ($env:HOME) {
		return Join-Path $env:HOME ".local\bin"
	}
	return Join-Path $HOME ".local\bin"
}

# <host>/<repo>/releases/latest redirects to the latest release's tag page, so
# the tag is read from that redirect. It is an ordinary web request, not a
# GitHub API call, so the unauthenticated API rate limit (which answers with
# HTTP 403) never applies. `aviary upgrade` uses the same URL scheme; see
# internal/update/release.go and keep the two in sync.
function Get-LatestTag {
	param(
		[string]$RepoName,
		[string]$Base
	)

	$request = [System.Net.WebRequest]::Create("$Base/$RepoName/releases/latest")
	$request.Method = "HEAD"
	$request.AllowAutoRedirect = $false
	$response = $request.GetResponse()
	try {
		$location = $response.Headers["Location"]
	} finally {
		$response.Close()
	}
	if ($location -notmatch '/releases/tag/([^/?#]+)$') {
		throw "Failed to resolve the latest release of $RepoName"
	}
	return $Matches[1]
}

function Get-DisplayPath {
	param([string]$Path)

	$homeCandidates = @($HOME, $env:HOME, $env:USERPROFILE) | Where-Object { $_ } | Select-Object -Unique
	foreach ($homeDir in $homeCandidates) {
		$normalizedHome = $homeDir.TrimEnd('\', '/')
		if ($Path.StartsWith($normalizedHome, [System.StringComparison]::OrdinalIgnoreCase)) {
			return "~" + $Path.Substring($normalizedHome.Length)
		}
	}

	return $Path
}

# Maps a raw architecture name to the Go architecture of a release asset.
# switch -Regex matches case-insensitively, so "AMD64", "X64", and "x64" all
# map to amd64.
function ConvertTo-GoArch {
	param([string]$Value)

	switch -Regex ("$Value".Trim()) {
		'^(amd64|x64)$' { return "amd64" }
		'^arm64$' { return "arm64" }
	}
	return $null
}

# Picks the release architecture from the first source that names a supported
# one: RuntimeInformation.OSArchitecture, then PROCESSOR_ARCHITEW6432 (set for
# 32-bit processes on 64-bit Windows), then PROCESSOR_ARCHITECTURE, then
# Is64BitOperatingSystem. Windows PowerShell 5.1 on .NET Framework often has
# no RuntimeInformation type, so OSArchitecture alone comes back empty (#1099).
function Resolve-GoArch {
	param(
		[string]$OSArchitecture,
		[string]$ProcessorArchiteW6432,
		[string]$ProcessorArchitecture,
		[bool]$Is64BitOperatingSystem
	)

	foreach ($candidate in @($OSArchitecture, $ProcessorArchiteW6432, $ProcessorArchitecture)) {
		$goArch = ConvertTo-GoArch -Value $candidate
		if ($goArch) {
			return $goArch
		}
	}
	if ($Is64BitOperatingSystem) {
		return "amd64"
	}
	throw "Unsupported architecture: '$OSArchitecture' (PROCESSOR_ARCHITEW6432=$ProcessorArchiteW6432, PROCESSOR_ARCHITECTURE=$ProcessorArchitecture, Is64BitOperatingSystem=$Is64BitOperatingSystem)"
}

function Get-OSArchitecture {
	$runtimeInformation = "System.Runtime.InteropServices.RuntimeInformation" -as [type]
	if (-not $runtimeInformation) {
		return ""
	}
	try {
		return "$($runtimeInformation::OSArchitecture)"
	} catch {
		return ""
	}
}

$goArch = Resolve-GoArch `
	-OSArchitecture (Get-OSArchitecture) `
	-ProcessorArchiteW6432 $env:PROCESSOR_ARCHITEW6432 `
	-ProcessorArchitecture $env:PROCESSOR_ARCHITECTURE `
	-Is64BitOperatingSystem ([Environment]::Is64BitOperatingSystem)

if (-not $Version) {
	$Version = Get-LatestTag -RepoName $Repo -Base $DownloadBase
}

$assetName = "aviary_${Version}_windows_${goArch}.tar.gz"
$downloadUrl = "$DownloadBase/$Repo/releases/download/$Version"
$assetUrl = "$downloadUrl/$assetName"

$binDir = Get-BinDir
$null = New-Item -ItemType Directory -Path $binDir -Force

$tempRoot = Join-Path ([System.IO.Path]::GetTempPath()) ("aviary-install-" + [guid]::NewGuid().ToString("N"))
$null = New-Item -ItemType Directory -Path $tempRoot -Force
$archivePath = Join-Path $tempRoot $assetName

try {
	Invoke-WebRequest -Uri $assetUrl -OutFile $archivePath

	$checksumsPath = Join-Path $tempRoot "checksums.txt"
	Invoke-WebRequest -Uri "$downloadUrl/checksums.txt" -OutFile $checksumsPath
	$expected = Get-Content $checksumsPath | ForEach-Object {
		$fields = $_ -split '\s+'
		if ($fields.Length -ge 2 -and $fields[1] -eq $assetName) { $fields[0] }
	} | Select-Object -First 1
	if (-not $expected) {
		throw "checksums.txt for $Version has no entry for $assetName"
	}
	$actual = (Get-FileHash -Algorithm SHA256 -Path $archivePath).Hash.ToLowerInvariant()
	if ($actual -ne $expected.ToLowerInvariant()) {
		throw "Checksum mismatch for ${assetName}: expected $expected, got $actual"
	}

	# Quick validation: ensure the downloaded file is a gzip archive (magic bytes 1f 8b)
	try {
		$bytes = [System.IO.File]::ReadAllBytes($archivePath)
		if ($bytes.Length -lt 2 -or $bytes[0] -ne 0x1f -or $bytes[1] -ne 0x8b) {
			$size = (Get-Item $archivePath).Length
			$previewLen = [Math]::Min(64, $bytes.Length)
			$hex = ($bytes[0..($previewLen-1)] | ForEach-Object { $_.ToString("x2") }) -join " "
			Write-Host "Downloaded asset is not a gzip archive (missing 1f 8b header)."
			Write-Host "Asset URL: $assetUrl"
			Write-Host "Downloaded file: $archivePath (size: $size bytes)"
			Write-Host "First $previewLen bytes: $hex"
			throw "Downloaded asset not a gzip archive"
		}
	} catch {
		throw
	}

	tar -xzf $archivePath -C $tempRoot

	$binarySource = Join-Path $tempRoot "aviary.exe"
	if (-not (Test-Path $binarySource)) {
		$found = Get-ChildItem -Path $tempRoot -Filter "aviary*" -File -Recurse -ErrorAction SilentlyContinue | Select-Object -First 1
		if ($found) { $binarySource = $found.FullName }
	}

	if (-not (Test-Path $binarySource)) {
		Write-Host "Expected binary not found in archive. Listing extracted files:"
		Get-ChildItem -Path $tempRoot -Recurse | ForEach-Object { Write-Host $_.FullName }
		throw "Binary not found in extracted archive."
	}

	# Validate Windows PE header (MZ) so we fail fast with diagnostics if a wrong artifact was downloaded
	try {
		$fs = [System.IO.File]::OpenRead($binarySource)
		$first2 = New-Object byte[] 2
		$fs.Read($first2,0,2) | Out-Null
		$fs.Close()
		if ($first2[0] -ne 0x4D -or $first2[1] -ne 0x5A) {
			$size = (Get-Item $binarySource).Length
			$bytes = [System.IO.File]::ReadAllBytes($binarySource)
			$previewLen = [Math]::Min(64, $bytes.Length)
			$hex = ($bytes[0..($previewLen-1)] | ForEach-Object { $_.ToString("x2") }) -join " "
			Write-Host "Downloaded binary is not a Windows PE file (missing 'MZ' header)."
			Write-Host "Asset URL: $assetUrl"
			Write-Host "Downloaded file: $binarySource (size: $size bytes)"
			Write-Host "First $previewLen bytes: $hex"
			throw "Downloaded binary not Windows PE"
		}
	} catch {
		throw
	}
	$binaryDest = Join-Path $binDir "aviary.exe"
	Copy-Item -Path $binarySource -Destination $binaryDest -Force

	$currentParts = @($env:Path -split ";" | Where-Object { $_ })
	if ($currentParts -notcontains $binDir) {
		$env:Path = "$binDir;$env:Path"
	}

	$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
	$userParts = @($userPath -split ";" | Where-Object { $_ })
	if ($userParts -notcontains $binDir) {
		$newUserPath = if ($userPath) { "$binDir;$userPath" } else { $binDir }
		[Environment]::SetEnvironmentVariable("Path", $newUserPath, "User")
	}

	# Install the gh-* skills the same way the app does. install-skills
	# refreshes skills already installed, so re-running the installer or
	# upgrading is safe.
	if ($SkipSkills) {
		$skillsStatus = "skipped"
	} elseif (-not (Get-Command npx -ErrorAction SilentlyContinue)) {
		$skillsStatus = "no-npx"
	} else {
		& $binaryDest install-skills
		$skillsStatus = if ($LASTEXITCODE -eq 0) { "installed" } else { "failed" }
	}

	$binaryDisplay = Get-DisplayPath -Path $binaryDest
	$reset = ""
	$boldWhite = ""
	if ($Host.UI.SupportsVirtualTerminal) {
		$reset = "$([char]27)[0m"
		$boldWhite = "$([char]27)[1;97m"
	}

	Write-Host ("Installed {0}aviary {1}{2} to {0}{3}{4}" -f $boldWhite, $Version, $reset, $binaryDisplay, $reset)
	Write-Host "PATH updated for this PowerShell session and persisted to the user environment."
	Write-Host ""
	switch ($skillsStatus) {
		"installed" { Write-Host "Installed Aviary's gh-* skills globally." }
		"skipped" { Write-Host ("Skipped skill installation; run {0}aviary install-skills{1} to install them later." -f $boldWhite, $reset) }
		"no-npx" { Write-Warning "Skills were not installed because npx was not found; install Node.js and run aviary install-skills." }
		default { Write-Warning "Skill installation failed; run aviary install-skills to retry." }
	}
	Write-Host ""
	Write-Host ("Run {0}aviary configure{1} to set up your Aviary configuration." -f $boldWhite, $reset)
	Write-Host ("Run {0}aviary service install{1} to set up and start the system service (optional)." -f $boldWhite, $reset)
} finally {
	if (Test-Path $tempRoot) {
		Remove-Item -Path $tempRoot -Recurse -Force
	}
}
