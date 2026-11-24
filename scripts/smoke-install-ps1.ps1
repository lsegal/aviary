# Installs Aviary the way the README documents it on Windows, with the
# installer at -InstallerUrl piped through irm | iex, and checks that the
# installed aviary.exe runs. .github/workflows/installer-smoke.yml runs it
# under both Windows PowerShell 5.1 and PowerShell 7 (#1100).
param(
	[Parameter(Mandatory = $true)][string]$InstallerUrl,
	[Parameter(Mandatory = $true)][string]$HomeDir,
	[string]$Tag
)

$ErrorActionPreference = "Stop"
Write-Host "PowerShell $($PSVersionTable.PSVersion) ($($PSVersionTable.PSEdition)): $InstallerUrl"

$env:HOME = $HomeDir
$env:AVIARY_SKIP_SKILLS = "1"
New-Item -ItemType Directory -Path $HomeDir -Force | Out-Null

irm $InstallerUrl | iex

$bin = Join-Path $HomeDir ".local\bin\aviary.exe"
$version = & $bin version
if ($LASTEXITCODE -ne 0) {
	throw "$bin version exited with $LASTEXITCODE"
}
Write-Host $version
if ($Tag -and -not "$version".Contains($Tag)) {
	throw "installed $version, expected $Tag"
}
