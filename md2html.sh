#!/usr/bin/env bash
#
# md2html.sh —— 将 Markdown 渲染为带 UTF-8 头部的完整 HTML
# 依赖：marked（npm install -g marked）
# 用法：./md2html.sh [输入.md ...]        不传参时默认处理 README.md 与 README.en.md
# 输出：与输入同名，扩展名改为 .html（如 README.en.md -> README.en.html）
#
set -euo pipefail

TITLE="${TITLE:-Canyou-v1.0.0}"

# 检查依赖
command -v marked >/dev/null 2>&1 || { echo "错误：未找到 marked，请先执行 npm install -g marked" >&2; exit 1; }

# 待处理文件：命令行传入则用传入的，否则默认中英两份
if [ "$#" -gt 0 ]; then
    INPUTS=("$@")
else
    INPUTS=("README.md" "README.en.md")
fi

# 根据文件名推断 lang：*.en.md 视为英文，其余为简体中文
lang_of() {
    case "$1" in
        *.en.md) echo "en" ;;
        *)       echo "zh-CN" ;;
    esac
}

for INPUT in "${INPUTS[@]}"; do
    [ -f "$INPUT" ] || { echo "错误：输入文件不存在：$INPUT" >&2; exit 1; }

    OUTPUT="${INPUT%.md}.html"
    LANG="$(lang_of "$INPUT")"

    # 生成带 UTF-8 头部的完整 HTML
    cat <<EOF > "$OUTPUT"
<!DOCTYPE html>
<html lang="${LANG}">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>${TITLE}</title>
</head>
<body>
EOF

    marked "$INPUT" >> "$OUTPUT"

    echo "</body></html>" >> "$OUTPUT"

    echo "已生成：$OUTPUT"
done
