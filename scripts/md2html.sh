#!/usr/bin/env bash
#
# md2html.sh —— 将 Markdown 渲染为带 UTF-8 头部的完整 HTML
# 依赖：marked（npm install -g marked）
# 用法：bash scripts/md2html.sh [输入.md ...]        不传参时默认处理 README.md 与 README.en.md
# 输出：仓库 dist/docs/ 下的同名 HTML，并携带 images/ 图片
#
set -euo pipefail

TITLE="${TITLE:-Canyou}"
ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_DIR="$ROOT/dist/docs"

# 检查依赖
command -v marked >/dev/null 2>&1 || { echo "错误：未找到 marked，请先执行 npm install -g marked" >&2; exit 1; }

# 待处理文件：命令行传入则用传入的，否则默认中英两份
if [ "$#" -gt 0 ]; then
    INPUTS=("$@")
else
    INPUTS=("$ROOT/README.md" "$ROOT/README.en.md")
fi

# 根据文件名推断 lang：*.en.md 视为英文，其余为简体中文
lang_of() {
    case "$1" in
        *.en.md) echo "en" ;;
        *)       echo "zh-CN" ;;
    esac
}

mkdir -p "$OUT_DIR/images"
cp -R "$ROOT/docs/images/." "$OUT_DIR/images/"
cp "$ROOT/LICENSE" "$OUT_DIR/LICENSE.txt"

for INPUT in "${INPUTS[@]}"; do
    [ -f "$INPUT" ] || { echo "错误：输入文件不存在：$INPUT" >&2; exit 1; }

    NAME="$(basename -- "$INPUT")"
    OUTPUT="$OUT_DIR/${NAME%.md}.html"
    LANG="$(lang_of "$INPUT")"

    # 生成带 UTF-8 头部的完整 HTML
    cat <<EOF > "$OUTPUT"
<!DOCTYPE html>
<html lang="${LANG}">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>${TITLE}</title>
<style>
html { background: #fff; color-scheme: light; }
body { box-sizing: border-box; max-width: 1120px; margin: 0 auto; padding: 24px; font: 16px/1.65 system-ui, sans-serif; color: #24292f; background: #fff; }
img { max-width: 100%; height: auto; vertical-align: top; }
a { color: #0969da; }
pre { overflow-x: auto; padding: 16px; background: #f6f8fa; border-radius: 6px; }
code { overflow-wrap: anywhere; }
table { display: block; max-width: 100%; overflow-x: auto; border-collapse: collapse; }
th, td { padding: 8px 12px; border: 1px solid #d0d7de; }
blockquote { margin-left: 0; padding-left: 16px; border-left: 4px solid #d0d7de; color: #57606a; }
details { margin: 20px 0; }
summary { cursor: pointer; font-weight: 600; }
@media (max-width: 600px) { body { padding: 16px; } }
</style>
</head>
<body>
EOF

    # HTML 中重定位 README 图片引用，源 Markdown 与仓库图片保持不变。
    marked "$INPUT" | sed -E 's#(src|href)="(\./)?docs/images/#\1="images/#g; s#href="(\./)?(README(\.en)?)\.md"#href="\2.html"#g; s#href="(\./)?LICENSE"#href="LICENSE.txt"#g' >> "$OUTPUT"

    echo "</body></html>" >> "$OUTPUT"

    echo "已生成：$OUTPUT"
done
