# stamp_version.ps1 - invoked by build.bat (also runnable directly from PowerShell).
# 1) Normalizes %VER% (trims surrounding whitespace and wrapping quotes, e.g. from
#    "set VER=v1.0.1 && build.bat" which leaves a trailing space).
# 2) Validates the version format, stamps FileVersion/ProductVersion into versioninfo.json
#    and refreshes the copyright year to the current build year.
# 3) Writes the normalized version to %VERFILE% (no trailing newline) so the batch
#    script can adopt it for the linker flag and the output file name.
# Requires: Windows PowerShell 5.1+. Keep this file ASCII-only: without a BOM,
# Windows PowerShell 5.1 decodes scripts using the ANSI code page.
$ErrorActionPreference = 'Stop'
try {
    $raw = [string]$env:VER
    $v = $raw.Trim().Trim('"')
    if (-not $v) { throw 'VER is empty after trimming; expected vX.Y.Z or dev' }

    $parts = @(0, 0, 0, 0)
    $label = 'dev'
    if ($v -ne 'dev') {
        $m = [regex]::Match($v, '^[vV]?([0-9]+)(?:\.([0-9]+))?(?:\.([0-9]+))?(?:\.([0-9]+))?((?:-[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?(?:\+[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?)$')
        if (-not $m.Success) { throw 'Invalid VER: expected vX.Y.Z, a prerelease version, or dev' }
        for ($i = 0; $i -lt 4; $i++) {
            if ($m.Groups[$i + 1].Success) { $parts[$i] = [uint16]::Parse($m.Groups[$i + 1].Value) }
        }
        $label = $parts -join '.'
        if ($m.Groups[5].Value) { $label = $v -replace '^[vV]', '' }
    }

    $jsonPath = Join-Path (Get-Location) 'versioninfo.json'
    $j = Get-Content -LiteralPath $jsonPath -Raw -Encoding UTF8 | ConvertFrom-Json
    $fixed = [ordered]@{ Major = $parts[0]; Minor = $parts[1]; Patch = $parts[2]; Build = $parts[3] }
    $j.FixedFileInfo.FileVersion = $fixed
    $j.FixedFileInfo.ProductVersion = $fixed
    $j.StringFileInfo.FileVersion = $label
    $j.StringFileInfo.ProductVersion = $label
    $year = [string](Get-Date).Year
    $j.StringFileInfo.LegalCopyright = ([regex]'\b[0-9]{4}\b').Replace($j.StringFileInfo.LegalCopyright, $year, 1)
    $text = ($j | ConvertTo-Json -Depth 10) + [Environment]::NewLine
    [IO.File]::WriteAllText($jsonPath, $text, (New-Object Text.UTF8Encoding($false)))

    $outFile = [string]$env:VERFILE
    if ($outFile) { [IO.File]::WriteAllText($outFile, $v, (New-Object Text.UTF8Encoding($false))) }
    exit 0
} catch {
    [Console]::Error.WriteLine($_.Exception.Message)
    exit 1
}
