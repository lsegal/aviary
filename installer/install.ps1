[CmdletBinding()]
param(
	[string]$Version = $env:AVIARY_VERSION,
	[string]$Repo = $(if ($env:AVIARY_REPO) { $env:AVIARY_REPO } else { "lsegal/aviary" }),
	[string]$ApiBase = $(if ($env:AVIARY_API_BASE) { $env:AVIARY_API_BASE } else { "https://api.github.com" }),
	[switch]$Yes,
	[switch]$SkipService
)

$ErrorActionPreference = "Stop"

# Global headers for diagnostics and API calls
$headers = @{ Accept = "application/vnd.github+json" }
function Get-ConfigRoot {
	if ($env:XDG_CONFIG_HOME) {
		return Join-Path $env:XDG_CONFIG_HOME "aviary"
	}
	if ($env:AVIARY_HOME) {
		return Join-Path $env:AVIARY_HOME ".config\aviary"
	}
	if ($env:HOME) {
		return Join-Path $env:HOME ".config\aviary"
	}
	return Join-Path $HOME ".config\aviary"
}

function Get-Release {
	param(
		[string]$RepoName,
		[string]$Tag,
		[string]$ApiRoot
	)

	$headers = @{ Accept = "application/vnd.github+json" }
	if ($Tag) {
		return Invoke-RestMethod -Headers $headers -Uri "$ApiRoot/repos/$RepoName/releases/tags/$Tag"
	}
	return Invoke-RestMethod -Headers $headers -Uri "$ApiRoot/repos/$RepoName/releases/latest"
}

switch ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture) {
	"X64" { $goArch = "amd64" }
	"Arm64" { $goArch = "arm64" }
	default { throw "Unsupported architecture: $([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture)" }
}

$release = Get-Release -RepoName $Repo -Tag $Version -ApiRoot $ApiBase
if (-not $Version) {
	$Version = $release.tag_name
}

$assetName = "aviary_${Version}_windows_${goArch}.tar.gz"
$asset = $release.assets | Where-Object { $_.name -eq $assetName } | Select-Object -First 1
if (-not $asset) {
	$assetUrl = "https://github.com/$Repo/releases/download/$Version/$assetName"
} else {
	$assetUrl = $asset.browser_download_url
}

$configRoot = Get-ConfigRoot
$binDir = Join-Path $configRoot "bin"
$null = New-Item -ItemType Directory -Path $binDir -Force

$tempRoot = Join-Path ([System.IO.Path]::GetTempPath()) ("aviary-install-" + [guid]::NewGuid().ToString("N"))
$null = New-Item -ItemType Directory -Path $tempRoot -Force
$archivePath = Join-Path $tempRoot $assetName

try {
	# Attempt a HEAD to show useful diagnostics before downloading
	try {
		$headResp = Invoke-WebRequest -Method Head -Uri $assetUrl -Headers $headers -ErrorAction SilentlyContinue
		if ($headResp -and $headResp.Headers) {
			Write-Host "Asset headers: Content-Type=$($headResp.Headers['Content-Type']) Content-Length=$($headResp.Headers['Content-Length'])"
		}
	} catch {}

	Invoke-WebRequest -Uri $assetUrl -OutFile $archivePath

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

	Write-Host "Installed aviary to $binaryDest"
	Write-Host "Version: $Version"
	Write-Host "PATH updated for this PowerShell session and persisted to the user environment."
	# Prompt to install the service
	if ($SkipService) {
		Write-Host "Skipping service installation."
	} elseif ($Yes) {
		& $binaryDest service install
	} else {
		$ans = Read-Host "Install aviary as a service? [Y/n]"
		if ([string]::IsNullOrWhiteSpace($ans) -or $ans -match '^[Yy]') {
			& $binaryDest service install
		} else {
			Write-Host "Skipping service installation."
		}
	}
} finally {
	if (Test-Path $tempRoot) {
		Remove-Item -Path $tempRoot -Recurse -Force
	}
}
