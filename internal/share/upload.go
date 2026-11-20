package share

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	// maxUploadBytes 单次上传请求的总大小上限（2 GB），超出即拒绝
	maxUploadBytes = 2 << 30
	// uploadMemoryBuffer 解析 multipart 时在内存中保留的字节数，超出部分转临时文件
	uploadMemoryBuffer = 32 << 20
)

// uploadResult 上传结果，以 JSON 返回给前端就地展示
type uploadResult struct {
	OK      bool     `json:"ok"`
	Message string   `json:"message"`
	Saved   []string `json:"saved"`
	Failed  []string `json:"failed"`
}

// handleFileUpload 接收 multipart 上传，将文件保存到 uploadDir，并以 JSON 返回结果供前端就地展示
func handleFileUpload(writer http.ResponseWriter, request *http.Request, uploadDir string) {
	if request.Method != http.MethodPost {
		writeUploadJSON(writer, http.StatusMethodNotAllowed, uploadResult{Message: "上传仅支持 POST"})
		return
	}
	if uploadDir == "" {
		writeUploadJSON(writer, http.StatusBadRequest, uploadResult{Message: "服务端未配置上传目录"})
		return
	}
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		writeUploadJSON(writer, http.StatusInternalServerError, uploadResult{Message: "上传目录不可用"})
		return
	}

	request.Body = http.MaxBytesReader(writer, request.Body, maxUploadBytes)
	if err := request.ParseMultipartForm(uploadMemoryBuffer); err != nil {
		writeUploadJSON(writer, http.StatusRequestEntityTooLarge, uploadResult{Message: "上传解析失败或文件过大"})
		return
	}
	defer request.MultipartForm.RemoveAll()

	headers := request.MultipartForm.File["file"]
	if len(headers) == 0 {
		writeUploadJSON(writer, http.StatusBadRequest, uploadResult{Message: "未选择任何文件"})
		return
	}

	var result uploadResult
	for _, header := range headers {
		savedName, err := saveUploadedFile(uploadDir, header)
		if err != nil {
			result.Failed = append(result.Failed, fmt.Sprintf("%s（%v）", filepath.Base(header.Filename), err))
			continue
		}
		result.Saved = append(result.Saved, savedName)
	}
	switch {
	case len(result.Saved) > 0 && len(result.Failed) == 0:
		result.OK, result.Message = true, "上传成功"
	case len(result.Saved) > 0:
		result.OK, result.Message = true, "部分文件上传成功"
	default:
		result.Message = "上传失败"
	}
	writeUploadJSON(writer, http.StatusOK, result)
}

// saveUploadedFile 保存单个上传文件：仅取基名防路径穿越，重名自动追加序号
func saveUploadedFile(uploadDir string, header *multipart.FileHeader) (string, error) {
	base := filepath.Base(header.Filename)
	if base == "" || base == "." || base == ".." || base == string(filepath.Separator) {
		return "", fmt.Errorf("非法文件名")
	}
	target, err := uniqueFilePath(filepath.Join(uploadDir, base))
	if err != nil {
		return "", err
	}

	source, err := header.Open()
	if err != nil {
		return "", err
	}
	defer source.Close()

	destination, err := os.Create(target)
	if err != nil {
		return "", err
	}
	defer destination.Close()

	if _, err := io.Copy(destination, source); err != nil {
		return "", err
	}
	return filepath.Base(target), nil
}

// uniqueFilePath 目标已存在时在主名后追加 (1)(2)… 直到不冲突
func uniqueFilePath(target string) (string, error) {
	exists, err := pathExists(target)
	if err != nil || !exists {
		return target, err
	}

	dir, name := filepath.Split(target)
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for index := 1; ; index++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s(%d)%s", stem, index, ext))
		exists, err := pathExists(candidate)
		if err != nil {
			return "", err
		}
		if !exists {
			return candidate, nil
		}
	}
}

// pathExists 判断路径是否存在
func pathExists(target string) (bool, error) {
	_, err := os.Stat(target)
	switch {
	case err == nil:
		return true, nil
	case os.IsNotExist(err):
		return false, nil
	default:
		return false, err
	}
}

// writeUploadJSON 以 JSON 输出上传结果
func writeUploadJSON(writer http.ResponseWriter, status int, result uploadResult) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	if err := json.NewEncoder(writer).Encode(result); err != nil {
		log.Println("共享服务异常:", err)
	}
}
