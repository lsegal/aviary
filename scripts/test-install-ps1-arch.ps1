# Checks installer/install.ps1's architecture detection (#1099) without
# running the installer: the script's functions are loaded from its syntax
# tree, so this runs under both Windows PowerShell 5.1 and PowerShell 7 on any
# OS.
$ErrorActionPreference = "Stop"

$installer = Join-Path $PSScriptRoot "..\installer\install.ps1"
$ast = [System.Management.Automation.Language.Parser]::ParseFile($installer, [ref]$null, [ref]$null)
$ast.FindAll({ param($node) $node -is [System.Management.Automation.Language.FunctionDefinitionAst] }, $false) |
	Where-Object { $_.Name -in @("ConvertTo-GoArch", "Resolve-GoArch", "Get-OSArchitecture") } |
	ForEach-Object { . ([scriptblock]::Create($_.Extent.Text)) }

$failures = @()

function Assert-Arch {
	param([string]$Expected, [hashtable]$Arguments)

	$actual = Resolve-GoArch @Arguments
	if ($actual -ne $Expected) {
		$script:failures += "expected $Expected, got '$actual' for $(($Arguments.GetEnumerator() | ForEach-Object { "$($_.Key)=$($_.Value)" }) -join ', ')"
	}
}

# OSArchitecture wins when present, whatever its case.
Assert-Arch "amd64" @{ OSArchitecture = "X64"; ProcessorArchitecture = "ARM64" }
Assert-Arch "arm64" @{ OSArchitecture = "Arm64"; ProcessorArchitecture = "AMD64" }
Assert-Arch "amd64" @{ OSArchitecture = "x64" }
Assert-Arch "arm64" @{ OSArchitecture = "ARM64" }
# Windows PowerShell 5.1 often has no OSArchitecture (#1099).
Assert-Arch "amd64" @{ OSArchitecture = ""; ProcessorArchitecture = "AMD64" }
Assert-Arch "arm64" @{ OSArchitecture = ""; ProcessorArchitecture = "ARM64" }
Assert-Arch "amd64" @{ OSArchitecture = "X86"; ProcessorArchitecture = "amd64" }
# A 32-bit process on 64-bit Windows reports the OS in PROCESSOR_ARCHITEW6432.
Assert-Arch "amd64" @{ ProcessorArchiteW6432 = "AMD64"; ProcessorArchitecture = "x86" }
Assert-Arch "arm64" @{ ProcessorArchiteW6432 = "ARM64"; ProcessorArchitecture = "x86" }
# Is64BitOperatingSystem is the last resort.
Assert-Arch "amd64" @{ ProcessorArchitecture = "x86"; Is64BitOperatingSystem = $true }

try {
	Resolve-GoArch -OSArchitecture "" -ProcessorArchitecture "x86" -Is64BitOperatingSystem $false
	$failures += "a 32-bit OS did not throw"
} catch {
	$expected = "Unsupported architecture: '' (PROCESSOR_ARCHITEW6432=, PROCESSOR_ARCHITECTURE=x86, Is64BitOperatingSystem=False)"
	if ($_.Exception.Message -ne $expected) {
		$failures += "unexpected error: $($_.Exception.Message)"
	}
}

# The detected architecture on this machine must resolve without throwing.
$detected = Resolve-GoArch `
	-OSArchitecture (Get-OSArchitecture) `
	-ProcessorArchiteW6432 $env:PROCESSOR_ARCHITEW6432 `
	-ProcessorArchitecture $env:PROCESSOR_ARCHITECTURE `
	-Is64BitOperatingSystem ([Environment]::Is64BitOperatingSystem)
Write-Host "PowerShell $($PSVersionTable.PSVersion): OSArchitecture='$(Get-OSArchitecture)', detected $detected"

if ($failures.Count -gt 0) {
	$failures | ForEach-Object { Write-Host "FAIL: $_" }
	exit 1
}
Write-Host "install.ps1 architecture detection: all checks passed"
