# Tests installer/install.ps1 on Windows. CI runs it under both Windows
# PowerShell 5.1 (powershell) and PowerShell 7 (pwsh), against the mock
# release server that scripts/mock-static-server.mjs serves at -DownloadBase.
param(
	[Parameter(Mandatory = $true)][string]$DownloadBase,
	[Parameter(Mandatory = $true)][string]$HomeDir
)

$ErrorActionPreference = "Stop"
$installer = Join-Path $PSScriptRoot "..\installer\install.ps1"
Write-Host "PowerShell $($PSVersionTable.PSVersion) ($($PSVersionTable.PSEdition))"

# Architecture detection (#1100): load Resolve-GoArch alone from the installer.
$ast = [System.Management.Automation.Language.Parser]::ParseFile($installer, [ref]$null, [ref]$null)
$resolveGoArch = $ast.Find({
	param($node)
	$node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq "Resolve-GoArch"
}, $true)
if (-not $resolveGoArch) {
	throw "Resolve-GoArch not found in $installer"
}
. ([scriptblock]::Create($resolveGoArch.Extent.Text))

function Assert-GoArch {
	param([string]$Expected, [hashtable]$Inputs)
	$actual = Resolve-GoArch @Inputs
	if ($actual -ne $Expected) {
		throw "Resolve-GoArch $(($Inputs.GetEnumerator() | ForEach-Object { "$($_.Key)=$($_.Value)" }) -join ' ') returned '$actual', expected '$Expected'"
	}
}

function Assert-GoArchError {
	param([string[]]$Contains, [hashtable]$Inputs)
	try {
		$actual = Resolve-GoArch @Inputs
	} catch {
		foreach ($text in $Contains) {
			if (-not $_.Exception.Message.Contains($text)) {
				throw "Resolve-GoArch error '$($_.Exception.Message)' does not contain '$text'"
			}
		}
		return
	}
	throw "Resolve-GoArch returned '$actual', expected an error"
}

Assert-GoArch "amd64" @{ OSArchitecture = "X64" }
Assert-GoArch "arm64" @{ OSArchitecture = "Arm64" }
Assert-GoArch "amd64" @{ ProcessorArchitecture = "AMD64" }
Assert-GoArch "amd64" @{ ProcessorArchitecture = "amd64" }
Assert-GoArch "arm64" @{ ProcessorArchitecture = "ARM64" }
# A 32-bit PowerShell on a 64-bit OS reports its own architecture in
# PROCESSOR_ARCHITECTURE and the OS's in PROCESSOR_ARCHITEW6432.
Assert-GoArch "amd64" @{ ProcessorArchitew6432 = "AMD64"; ProcessorArchitecture = "x86" }
Assert-GoArch "arm64" @{ ProcessorArchitew6432 = "ARM64"; ProcessorArchitecture = "x86" }
Assert-GoArch "amd64" @{ Is64BitOperatingSystem = $true }
Assert-GoArchError @("Unsupported architecture: x86", "PROCESSOR_ARCHITECTURE='x86'") @{ ProcessorArchitecture = "x86" }
Assert-GoArchError @("OSArchitecture=''", "PROCESSOR_ARCHITECTURE=''", "Is64BitOperatingSystem=False") @{}
Write-Host "Resolve-GoArch: ok"

# The mock release only has the amd64 asset, which every windows-latest
# runner needs.
$env:HOME = $HomeDir
$bin = Join-Path $HomeDir ".local\bin\aviary.exe"
New-Item -ItemType Directory -Path $HomeDir -Force | Out-Null

& $installer -Repo lsegal/aviary -DownloadBase $DownloadBase -SkipSkills
if (-not (Test-Path $bin)) {
	throw "installer did not create $bin"
}
if (-not (($env:Path -split ';') -contains (Split-Path $bin))) {
	throw "installer did not prepend the bin directory to PATH"
}
Remove-Item $bin

# Piped through irm | iex like the documented command, configured through
# the environment.
$env:AVIARY_DOWNLOAD_BASE = $DownloadBase
$env:AVIARY_SKIP_SKILLS = "1"
irm "$DownloadBase/install.ps1" | iex
if (-not (Test-Path $bin)) {
	throw "piped installer did not create $bin"
}
Write-Host "install.ps1: ok"
