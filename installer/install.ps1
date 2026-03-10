[CmdletBinding()]
param(
	[string]$Version = $env:AVIARY_VERSION,
	[string]$Repo = $(if ($env:AVIARY_REPO) { $env:AVIARY_REPO } else { "lsegal/aviary" }),
	[string]$ApiBase = $(if ($env:AVIARY_API_BASE) { $env:AVIARY_API_BASE } else { "https://api.github.com" })
)

$ErrorActionPreference = "Stop"

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
	Invoke-WebRequest -Uri $assetUrl -OutFile $archivePath
	tar -xzf $archivePath -C $tempRoot
	$binarySource = Join-Path $tempRoot "aviary.exe"
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
} finally {
	if (Test-Path $tempRoot) {
		Remove-Item -Path $tempRoot -Recurse -Force
	}
}
