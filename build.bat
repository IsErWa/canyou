@echo off
setlocal EnableExtensions DisableDelayedExpansion
REM Canyou 构建脚本：关闭 CGO，去除调试符号，以 GUI 子系统(-H=windowsgui)输出单文件 Canyou-<版本号>.exe（不分配控制台窗口）
REM 图标与版本信息（描述/公司/版权等）由 versioninfo.json 经 goversioninfo 生成 resource.syso 自动嵌入
REM   首次需安装：go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@latest
REM CMD set VER=v1.0.1 && build.bat
REM PowerShell $env:VER="v1.0.1"; .\build.bat
REM github打版 git tag v1.0.1 && git push origin v1.0.1
set CGO_ENABLED=0
REM 解析版本号：优先用环境变量 VER（CI 传入标签），否则取最近的 git 标签，都没有则 dev
if not defined VER (
    for /f "delims=" %%v in ('git describe --tags "--abbrev=0" 2^>nul') do set "VER=%%v"
)
if not defined VER set "VER=dev"
REM 先同步四处版本字段，再生成资源；预发布后缀保留在文字版本，数字版本限定为 0-65535。
powershell.exe -NoLogo -NoProfile -NonInteractive -Command ^
    "$ErrorActionPreference = 'Stop'; $v = $env:VER; $parts = @(0,0,0,0); $label = 'dev';" ^
    "if ($v -ne 'dev') { $m = [regex]::Match($v, '^[vV]?([0-9]+)(?:\.([0-9]+))?(?:\.([0-9]+))?(?:\.([0-9]+))?((?:-[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?(?:\+[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?)$'); if (-not $m.Success) { throw 'Invalid VER: expected vX.Y.Z, a prerelease version, or dev' }; for ($i = 0; $i -lt 4; $i++) { if ($m.Groups[$i+1].Success) { $parts[$i] = [uint16]::Parse($m.Groups[$i+1].Value) } }; $label = $parts -join '.'; if ($m.Groups[5].Value) { $label = $v -replace '^[vV]', '' } };" ^
    "$j = Get-Content -LiteralPath 'versioninfo.json' -Raw -Encoding UTF8 | ConvertFrom-Json; $fixed = [ordered]@{ Major = $parts[0]; Minor = $parts[1]; Patch = $parts[2]; Build = $parts[3] };" ^
    "$j.FixedFileInfo.FileVersion = $fixed; $j.FixedFileInfo.ProductVersion = $fixed; $j.StringFileInfo.FileVersion = $label; $j.StringFileInfo.ProductVersion = $label;" ^
    "$year = [string](Get-Date).Year; $j.StringFileInfo.LegalCopyright = ([regex]'\b[0-9]{4}\b').Replace($j.StringFileInfo.LegalCopyright, $year, 1);" ^
    "$json = ($j | ConvertTo-Json -Depth 10) + [Environment]::NewLine; [IO.File]::WriteAllText((Join-Path (Get-Location) 'versioninfo.json'), $json, (New-Object Text.UTF8Encoding($false)))"
if %errorlevel% neq 0 (
    echo 同步版本号失败，请检查 VER 格式、数字范围及 versioninfo.json
    exit /b 1
)
goversioninfo -64
if %errorlevel% neq 0 (
    echo 生成版本信息资源失败（请确认已安装 goversioninfo 且存在 versioninfo.json）
    exit /b 1
)
go build -trimpath -ldflags "-s -w -H=windowsgui -X main.Version=%VER%" -o "Canyou-%VER%.exe" .
if %errorlevel% neq 0 (
    echo 构建失败
    exit /b 1
)
echo 构建完成: Canyou-%VER%.exe
