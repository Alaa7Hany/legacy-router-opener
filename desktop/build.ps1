[CmdletBinding()]
param(
    [string]$Version = "0.0.0-dev"
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

# The embedded version never carries a leading "v"/"V" — keep in sync with
# build.sh and the Makefile.
$trimmedVersion = ($Version.Trim() -replace '^[vV]', '')
if ([string]::IsNullOrWhiteSpace($trimmedVersion)) {
    throw "Version cannot be empty."
}

$coreVersion = $trimmedVersion -split '[-+]', 2 | Select-Object -First 1
$rawParts = $coreVersion.Split('.')
if ($rawParts.Count -lt 1 -or $rawParts.Count -gt 4) {
    throw "Cannot derive 4-part Windows FileVersion from '$Version'."
}

$numericParts = New-Object System.Collections.Generic.List[string]
foreach ($part in $rawParts) {
    $parsed = 0
    if (-not [uint16]::TryParse($part, [ref]$parsed)) {
        throw "Invalid numeric version segment '$part' in '$Version'."
    }
    $numericParts.Add($parsed.ToString())
}
while ($numericParts.Count -lt 4) {
    $numericParts.Add("0")
}
$fileVersion = $numericParts -join "."

$sysoFile = "rsrc_windows_amd64.syso"

Push-Location $PSScriptRoot
try {
    Remove-Item -Force -ErrorAction SilentlyContinue $sysoFile

    & go run github.com/tc-hib/go-winres@v0.3.3 make `
        --in winres/winres.json `
        --product-version $fileVersion `
        --file-version $fileVersion `
        --arch amd64
    if ($LASTEXITCODE -ne 0) {
        throw "go-winres failed with exit code $LASTEXITCODE."
    }

    $ldflags = "-s -w -H=windowsgui -X main.version=$trimmedVersion"
    & go build -trimpath "-ldflags=$ldflags" -o legacy-router-opener.exe .
    if ($LASTEXITCODE -ne 0) {
        throw "go build failed with exit code $LASTEXITCODE."
    }
}
finally {
    Remove-Item -Force -ErrorAction SilentlyContinue $sysoFile
    Pop-Location
}
