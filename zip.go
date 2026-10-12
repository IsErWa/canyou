package main

import (
	"archive/zip"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// handleZipDownload 将 viewDir 下对应的文件/目录实时流式打包为 zip 写回响应；不落盘、不缓存
// POST /zip  base=当前查看目录  paths=... 可重复（单文件夹与批量选择共用此入口）
// zip 条目以 base（当前查看目录）为根命名，而非共享目录 viewDir 顶层
func handleZipDownload(writer http.ResponseWriter, request *http.Request, viewDir string) {
	if request.Method != http.MethodPost {
		http.Error(writer, "打包下载仅支持 POST", http.StatusMethodNotAllowed)
		return
	}
	if err := request.ParseForm(); err != nil {
		http.Error(writer, "请求参数解析失败", http.StatusBadRequest)
		return
	}

	// base：当前查看目录，作为 zip 命名根；缺省或非法越界时回退到 viewDir 自身
	baseDir, ok := resolveWithinDir(viewDir, request.FormValue("base"))
	if !ok {
		http.Error(writer, "非法路径", http.StatusBadRequest)
		return
	}

	var rawPaths []string
	for _, field := range request.Form["paths"] {
		rawPaths = append(rawPaths, strings.Split(field, ",")...)
	}

	var targets []string
	for _, raw := range rawPaths {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		full, ok := resolveWithinDir(viewDir, raw)
		if !ok {
			http.Error(writer, "非法路径", http.StatusBadRequest)
			return
		}
		// 目标必须位于 base 之内，避免 zip 条目路径逃逸出当前查看目录
		relToBase, err := filepath.Rel(baseDir, full)
		if err != nil || relToBase == "." || relToBase == ".." || strings.HasPrefix(relToBase, ".."+string(filepath.Separator)) {
			http.Error(writer, "非法路径", http.StatusBadRequest)
			return
		}
		if _, err := os.Lstat(full); err != nil {
			http.Error(writer, "文件不存在", http.StatusNotFound)
			return
		}
		targets = append(targets, full)
	}
	if len(targets) == 0 {
		http.Error(writer, "未选择任何文件", http.StatusBadRequest)
		return
	}

	writer.Header().Set("Content-Type", "application/octet-stream")
	writer.Header().Set("Content-Disposition", `attachment; filename="Canyou-`+time.Now().Format("20060102-150405")+`.zip"`)
	if flusher, ok := writer.(http.Flusher); ok {
		flusher.Flush() // 立即下发响应头，浏览器第一时间弹出下载确认，随后边打包边传输
	}
	zipWriter := zip.NewWriter(writer)
	for _, target := range targets {
		if err := appendZipTarget(zipWriter, baseDir, target); err != nil {
			logServiceFailure(err) // zip 头可能已写出，无法再改写响应，仅记录
			break
		}
	}
	if err := zipWriter.Close(); err != nil {
		logServiceFailure(err)
	}
}

// resolveWithinDir 将 URL 路径解析为 viewDir 内的绝对路径（允许解析到 viewDir 自身）；越界时返回 false
func resolveWithinDir(viewDir, rawURLPath string) (string, bool) {
	name := strings.TrimPrefix(filepath.ToSlash(filepath.Clean("/"+rawURLPath)), "/")
	full := filepath.Join(viewDir, filepath.FromSlash(name))
	rel, err := filepath.Rel(viewDir, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return full, true
}

// appendZipTarget 将单个文件或目录（递归）写入 zip，条目名相对 baseDir（当前查看目录）保留层级
func appendZipTarget(zipWriter *zip.Writer, baseDir, target string) error {
	info, err := os.Lstat(target)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return appendZipFile(zipWriter, baseDir, target, info)
	}

	return filepath.Walk(target, func(walkPath string, walkInfo os.FileInfo, err error) error {
		if err != nil {
			return nil // 单个条目不可读时跳过，不中断整个打包
		}
		if walkInfo.IsDir() || walkInfo.Mode()&os.ModeSymlink != 0 {
			return nil // 空目录与符号链接不产出文件条目，避免环路与越界
		}
		return appendZipFile(zipWriter, baseDir, walkPath, walkInfo)
	})
}

// appendZipFile 以流式 deflate 写入单个文件条目，条目名相对 baseDir
func appendZipFile(zipWriter *zip.Writer, baseDir, filePath string, info os.FileInfo) error {
	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(baseDir, filePath)
	if err != nil {
		return err
	}
	header.Name = filepath.ToSlash(rel)
	header.Method = zip.Deflate
	header.Modified = info.ModTime()

	destination, err := zipWriter.CreateHeader(header)
	if err != nil {
		return err
	}
	source, err := os.Open(filePath)
	if err != nil {
		return nil // 打开失败仅跳过该文件
	}
	defer source.Close()

	_, err = io.Copy(destination, source)
	return err
}
