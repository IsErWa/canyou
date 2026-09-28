@echo off
REM Canyou 构建脚本：关闭 CGO，去除调试符号，以 GUI 子系统(-H=windowsgui)输出单文件 Canyou-<版本号>.exe（不分配控制台窗口）
REM 图标与版本信息（描述/公司/版权等）由 versioninfo.json 经 goversioninfo 生成 resource.syso 自动嵌入
REM   首次需安装：go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@latest
REM set VER=v1.0.0 && build.bat
REM git tag v1.0.1 && git push origin v1.0.1
set CGO_ENABLED=0
goversioninfo -64
if %errorlevel% neq 0 (
    echo 生成版本信息资源失败（请确认已安装 goversioninfo 且存在 versioninfo.json）
    exit /b 1
)
REM 解析版本号：优先用环境变量 VER（CI 传入标签），否则取最近的 git 标签，都没有则 dev
if not defined VER (
    for /f "delims=" %%v in ('git describe --tags --abbrev=0 2^>nul') do set "VER=%%v"
)
if not defined VER set "VER=dev"
go build -trimpath -ldflags "-s -w -H=windowsgui -X main.Version=%VER%" -o Canyou-%VER%.exe .
if %errorlevel% neq 0 (
    echo 构建失败
    exit /b 1
)
echo 构建完成: Canyou-%VER%.exe
