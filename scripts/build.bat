@echo off
setlocal EnableExtensions DisableDelayedExpansion
REM Canyou build script: CGO off, strip symbols, GUI subsystem (-H=windowsgui), single-file Canyou-<version>.exe
REM Keep every executed line below ASCII-only: cmd mis-tracks batch offsets if chcp changes the code page
REM while the file still contains multi-byte characters, and echo output is garbled under non-UTF8 code pages.
REM Version stamping and the copyright year refresh live in stamp_version.ps1 (safe Unicode error reporting there).
REM   goversioninfo is required once: go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@latest
REM CMD usage:        set "VER=v2.0.0" & build.bat
REM PowerShell usage: $env:VER="v2.0.0"; .\build.bat
REM GitHub release:   git tag v2.0.0-go-pc; git push origin v2.0.0-go-pc   (only v* tags trigger release.yml)
pushd "%~dp0.."
if errorlevel 1 exit /b 1
set CGO_ENABLED=0
if not defined VER (
    for /f "delims=" %%v in ('git describe --tags "--abbrev=0" 2^>nul') do set "VER=%%v"
)
if not defined VER set "VER=dev"
REM stamp_version.ps1 normalizes VER (trims blanks/quotes), validates it, updates versioninfo.json,
REM and writes the normalized version to VERFILE so it is used for -X main.Version and the exe name.
if not defined VERFILE set "VERFILE=%TEMP%\canyou-ver-%RANDOM%%RANDOM%.txt"
if exist "%VERFILE%" del "%VERFILE%" >nul 2>&1
powershell.exe -NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "%~dp0stamp_version.ps1"
if %errorlevel% neq 0 (
    echo [build] version stamping failed, see the message above
    goto :fail
)
for /f "usebackq delims=" %%v in ("%VERFILE%") do set "VER=%%v"
del "%VERFILE%" >nul 2>&1
if not exist "dist\bin" mkdir "dist\bin"
if errorlevel 1 goto :fail
goversioninfo -64 -icon "internal/assets/favicon-128x128.ico" -o "cmd/canyou/resource.syso" "build/windows/versioninfo.json"
if %errorlevel% neq 0 (
    echo [build] goversioninfo failed, ensure it is installed and versioninfo.json exists
    goto :fail
)
go build -trimpath -ldflags "-s -w -H=windowsgui -X main.Version=%VER%" -o "dist/bin/Canyou-%VER%.exe" ./cmd/canyou
if %errorlevel% neq 0 (
    echo [build] go build failed
    goto :fail
)
echo [build] done: dist/bin/Canyou-%VER%.exe
popd
endlocal & exit /b 0

:fail
if exist "%VERFILE%" del "%VERFILE%" >nul 2>&1
popd
endlocal & exit /b 1
