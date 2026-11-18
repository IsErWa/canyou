package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

const (
	// maxUploadBytes 单次上传请求的总大小上限（2 GB），超出即拒绝
	maxUploadBytes = 2 << 30
	// uploadMemoryBuffer 解析 multipart 时在内存中保留的字节数，超出部分转临时文件
	uploadMemoryBuffer = 32 << 20
)

// listingData 单页壳模板数据：目录内容不再服务端渲染，由前端 POST /api/list 获取
type listingData struct {
	CanUpload bool
	CanChat   bool
	ClientIP  string
}

// listingItem 目录列表接口中的单个条目
type listingItem struct {
	Name        string `json:"name"`
	Link        string `json:"link"`
	Path        string `json:"path"`
	Icon        string `json:"icon"`
	Size        string `json:"size"`
	ModTime     string `json:"modTime"`
	IsDirectory bool   `json:"isDir"`
}

// listResponse /api/list 目录列表响应
type listResponse struct {
	OK      bool          `json:"ok"`
	Path    string        `json:"path,omitempty"`
	Parent  string        `json:"parent,omitempty"`
	Items   []listingItem `json:"items"`
	Message string        `json:"message,omitempty"`
}

// uploadResult 上传结果，以 JSON 返回给前端就地展示
type uploadResult struct {
	OK      bool     `json:"ok"`
	Message string   `json:"message"`
	Saved   []string `json:"saved"`
	Failed  []string `json:"failed"`
}

var listingTemplate = template.Must(template.New("listing").Parse(listingHTML + chatTemplates))

// isDirectoryPath 判断请求路径映射到 viewDir 下是否为目录
func isDirectoryPath(viewDir, urlPath string) bool {
	name := filepath.FromSlash(path.Clean("/" + urlPath))
	info, err := os.Stat(filepath.Join(viewDir, name))
	return err == nil && info.IsDir()
}

// listingEntry 待排序的目录条目：保留 FileInfo 以便按类型与修改时间排序
type listingEntry struct {
	info os.FileInfo
}

// sortListingEntries 目录优先，同组内按修改时间从新到旧，时间相同时按名称升序
func sortListingEntries(entries []listingEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i].info, entries[j].info
		if a.IsDir() != b.IsDir() {
			return a.IsDir()
		}
		if modDiff := a.ModTime().Compare(b.ModTime()); modDiff != 0 {
			return modDiff > 0
		}
		return a.Name() < b.Name()
	})
}

// buildListing 读取目录并生成排序后的列表条目；reqPath 必须是 path.Clean("/"+p) 归一化后的路径
func buildListing(viewDir, reqPath string) ([]listingItem, error) {
	entries, err := os.ReadDir(filepath.Join(viewDir, filepath.FromSlash(reqPath)))
	if err != nil {
		return nil, err
	}

	var sortable []listingEntry
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue // 条目信息不可读（如已被删除）时跳过
		}
		sortable = append(sortable, listingEntry{info: info})
	}
	sortListingEntries(sortable)

	items := make([]listingItem, 0, len(sortable))
	for _, item := range sortable {
		var size string
		modTime := item.info.ModTime().Format("2006-01-02 15:04")
		if !item.info.IsDir() {
			size = humanSize(item.info.Size())
		}
		icon := "📄"
		if item.info.IsDir() {
			icon = "📁"
		}
		items = append(items, listingItem{
			Name: item.info.Name(), Link: joinURL(reqPath, item.info.Name(), item.info.IsDir()),
			Path: joinURL(reqPath, item.info.Name(), false),
			Icon: icon, Size: size, ModTime: modTime, IsDirectory: item.info.IsDir(),
		})
	}
	return items, nil
}

// serveListAPI POST /api/list：以 JSON 返回目录列表，供单页前端动态渲染
func serveListAPI(writer http.ResponseWriter, request *http.Request, viewDir string) {
	if request.Method != http.MethodPost {
		writeListJSON(writer, http.StatusMethodNotAllowed, listResponse{Message: "目录列表仅支持 POST"})
		return
	}
	var body struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(io.LimitReader(request.Body, 1<<16)).Decode(&body); err != nil {
		writeListJSON(writer, http.StatusBadRequest, listResponse{Message: "请求体格式错误"})
		return
	}
	reqPath := path.Clean("/" + body.Path) // 强制根路径 + Clean：穿越一律限制在 viewDir 内
	info, err := os.Stat(filepath.Join(viewDir, filepath.FromSlash(reqPath)))
	if err != nil || !info.IsDir() {
		writeListJSON(writer, http.StatusNotFound, listResponse{Message: "目录不存在"})
		return
	}
	items, err := buildListing(viewDir, reqPath)
	if err != nil {
		writeListJSON(writer, http.StatusInternalServerError, listResponse{Message: "读取目录失败"})
		return
	}
	response := listResponse{OK: true, Path: reqPath, Items: items}
	if reqPath != "/" {
		response.Parent = path.Dir(reqPath)
	}
	writeListJSON(writer, http.StatusOK, response)
}

// writeListJSON 输出目录列表接口的 JSON 响应
func writeListJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(status)
	if err := json.NewEncoder(writer).Encode(value); err != nil {
		logServiceFailure(err)
	}
}

// serveIndexPage 渲染单页壳：不再读盘列目录，列表由前端 POST /api/list 获取
func serveIndexPage(writer http.ResponseWriter, request *http.Request, uploadDir string, canChat bool) {
	data := listingData{CanUpload: uploadDir != "", CanChat: canChat, ClientIP: chatPeerIP(request)}
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := listingTemplate.Execute(writer, data); err != nil {
		logServiceFailure(err) // 响应已部分写出，仅记录
	}
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
		logServiceFailure(err)
	}
}

// joinURL 拼接子项访问链接，目录以 / 结尾
func joinURL(base, name string, isDir bool) string {
	link := path.Join(base, name)
	if isDir {
		link += "/"
	}
	return link
}

// humanSize 将字节数格式化为易读单位
func humanSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(size)/float64(div), "KMGTPE"[exp])
}

const listingHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<meta name="theme-color" id="themeColor" content="#f4f6fa">
<title>Canyou 共享</title>
{{template "themeBootstrap"}}
<style>
{{template "themeStyles"}}
*{box-sizing:border-box}
[hidden]{display:none!important}
html,body{height:100%;overflow:hidden}
body{margin:0;background:var(--bg);color:var(--text);font-size:14px;line-height:1.5;-webkit-font-smoothing:antialiased;
  font-family:-apple-system,BlinkMacSystemFont,"Segoe UI","Microsoft YaHei UI",Roboto,Helvetica,Arial,sans-serif}
a{color:inherit;text-decoration:none}
button,input,textarea{font:inherit}
button,a,input,textarea{-webkit-tap-highlight-color:transparent}
:focus-visible{outline:3px solid var(--accent);outline-offset:3px}
.icon{width:20px;height:20px;flex:none;fill:none;stroke:currentColor;stroke-width:1.7;stroke-linecap:round;stroke-linejoin:round;vertical-align:middle}
.wrap{--edge:24px;width:100%;max-width:1600px;height:100vh;height:100dvh;margin:auto;padding:env(safe-area-inset-top) max(var(--edge),env(safe-area-inset-right)) max(var(--edge),env(safe-area-inset-bottom)) max(var(--edge),env(safe-area-inset-left));display:flex;flex-direction:column;gap:24px}
.topbar{display:flex;align-items:center;gap:12px;min-height:64px;flex:none;border-bottom:1px solid var(--border)}
.tb-titles{min-width:0}.tb-actions{margin-left:auto;display:flex;align-items:center;gap:8px;flex:none}
.logo{width:40px;height:40px;border-radius:12px;flex:none;display:grid;place-items:center;background:var(--accent);color:var(--accent-ink)}
.logo .icon{width:24px;height:24px}.title{font-size:19px;font-weight:700;letter-spacing:-.5px}
.sub{font-size:12px;color:var(--muted)}
.card{background:var(--card);border:1px solid var(--border);border-radius:12px;box-shadow:var(--shadow)}
.workspace{display:grid;grid-template-columns:minmax(0,1fr);gap:24px;flex:1;min-height:0}
body.panel-open .workspace{grid-template-columns:minmax(0,1fr) 360px}
.view-nav{display:none}.badge{font-size:11px;min-width:20px;padding:1px 6px;border-radius:12px;background:var(--accent);color:var(--accent-ink);text-align:center}
{{template "chatStyles" .}}
.upload-dialog{width:min(560px,calc(100% - 32px));max-height:calc(100vh - 48px);max-height:calc(100dvh - 48px);padding:0;color:var(--text);overflow:auto}
.upload-dialog::backdrop,.lightbox::backdrop{background:rgba(12,20,38,.56);backdrop-filter:blur(3px)}
.upload{padding:24px}.upload-dialog:not([open]){display:none}
body.modal-open .wrap{pointer-events:none}
.upload .up-head{display:flex;align-items:center;justify-content:space-between;margin-bottom:12px}
.upload .up-head h2{font-size:15px;margin:0;font-weight:650}
.up-close{appearance:none;border:0;background:transparent;color:var(--muted);font-size:15px;cursor:pointer;
  line-height:1;padding:5px 8px;border-radius:9px;transition:.15s;font-family:inherit}
.up-close:hover{background:var(--accent-soft);color:var(--accent)}
.up-result{margin-top:12px;padding:10px 12px;border-radius:12px;font-size:13px;border:1px solid var(--border);background:var(--bg);word-break:break-all}
.up-result[hidden]{display:none}
.up-result.ok{border-color:var(--ok);color:var(--ok)}
.up-result.partial{border-color:var(--accent);color:var(--accent)}
.up-result.bad{border-color:var(--bad);color:var(--bad)}
.up-result ul{margin:6px 0 0;padding-left:18px;max-height:120px;overflow:auto}
.dz{position:relative;display:flex;flex-direction:column;align-items:center;justify-content:center;gap:6px;text-align:center;
  padding:30px 18px;border:1.6px dashed var(--border);border-radius:14px;cursor:pointer;transition:.15s;background:var(--bg)}
.dz:hover,.dz.over{border-color:var(--accent);background:var(--accent-soft)}
.dz .ic{font-size:28px}
.dz .t1{font-weight:600}
.dz .t1 b{color:var(--accent)}
.dz .t2{font-size:12.5px;color:var(--muted)}
.file-input{position:absolute;width:1px;height:1px;opacity:0;clip:rect(0 0 0 0);overflow:hidden}
.chips{display:flex;flex-direction:column;gap:8px;margin-top:14px}
.chip{display:flex;align-items:center;gap:10px;padding:8px 8px 8px 12px;border-radius:12px;font-size:13px;
  background:var(--bg);border:1px solid var(--border)}
.chip .nm{flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.chip .sz{color:var(--muted);flex:none;font-variant-numeric:tabular-nums}
.chip-x{appearance:none;border:0;background:transparent;color:var(--muted);cursor:pointer;font-size:14px;line-height:1;
  width:26px;height:26px;flex:none;border-radius:8px;display:grid;place-items:center;transition:.15s;font-family:inherit}
.chip-x:hover{background:var(--accent-soft);color:var(--accent)}
.pager{display:flex;align-items:center;justify-content:center;gap:14px;margin-top:12px}
.pager[hidden]{display:none}
.pager-info{font-size:12.5px;color:var(--muted);font-variant-numeric:tabular-nums;min-width:44px;text-align:center}
.actions{display:flex;align-items:center;justify-content:space-between;gap:14px;margin-top:16px;flex-wrap:wrap}
.btn{appearance:none;border:0;border-radius:12px;padding:11px 22px;font-size:14px;font-weight:650;cursor:pointer;
  background:var(--accent);color:var(--accent-ink);transition:.15s;box-shadow:var(--shadow)}
.btn:hover{filter:brightness(1.06)}
.btn:disabled{opacity:.45;cursor:not-allowed;box-shadow:none}
.hint{font-size:12.5px;color:var(--muted)}
.files{min-width:0;min-height:0;display:flex;flex-direction:column;overflow:hidden}
.files .head{padding:20px 24px 16px;flex:none}.files h1{font-size:22px;letter-spacing:-.6px;margin:0 0 12px;font-weight:700}
.pathbar{display:flex;align-items:center;gap:8px;min-width:0}.breadcrumbs{display:flex;align-items:center;gap:4px;overflow:auto;white-space:nowrap;min-width:0;color:var(--muted);scrollbar-width:thin}
.breadcrumbs button{border:0;background:none;color:inherit;cursor:pointer;padding:6px 8px;border-radius:6px;flex:none}.breadcrumbs button:hover{color:var(--accent);background:var(--accent-soft)}
.breadcrumbs [aria-current]{color:var(--text);font-weight:600}
.file-toolbar{display:flex;align-items:center;gap:12px;padding:12px 24px;border-top:1px solid var(--border);border-bottom:1px solid var(--border);flex:none}
.file-toolbar>.hint{margin-right:auto;white-space:nowrap}.tools{display:flex;align-items:center;gap:8px}
#fileList{flex:1;min-height:0;overflow:auto;overscroll-behavior:contain;scrollbar-width:thin;scrollbar-color:var(--border) transparent}
.row,.columns{display:grid;grid-template-columns:36px minmax(0,1fr) 72px 130px 44px;align-items:center;gap:12px;padding:8px 20px;border-bottom:1px solid var(--border)}
.columns{font-size:12px;color:var(--muted);min-height:40px;background:var(--bg);flex:none}.columns .col-size,.columns .col-date{text-align:right}
.row{min-height:64px}.row:last-child{border-bottom:0}.row:hover{background:var(--bg)}.row.selected{background:var(--accent-soft)}
.row .ck{display:flex;align-items:center;justify-content:center;min-height:36px;cursor:pointer}
.row input[type=checkbox]{width:18px;height:18px;accent-color:var(--accent);cursor:pointer;margin:0}
.row .name{display:flex;align-items:center;gap:12px;min-width:0;font-weight:500}.row .fi{display:grid;place-items:center;width:32px;height:36px;color:var(--accent);flex:none}
.row .fi.folder{color:#b58228}.row .name a{overflow:hidden;text-overflow:ellipsis;white-space:nowrap;min-width:0;padding:8px 0}
.row .name a:hover{color:var(--accent)}.file-meta{display:contents}
.row .size,.row .date{color:var(--muted);font-size:12px;white-space:nowrap;font-variant-numeric:tabular-nums;text-align:right}
.row .dl{display:grid;place-items:center;width:36px;height:36px;justify-self:end;color:var(--muted);border-radius:8px;flex:none}
.row .dl:hover{background:var(--accent-soft);color:var(--accent)}
.tbtn{appearance:none;border:1px solid var(--border);border-radius:8px;min-height:36px;padding:6px 12px;font-size:13px;font-weight:550;cursor:pointer;
  background:var(--card);color:var(--text);transition:background .15s;white-space:nowrap;display:inline-flex;align-items:center;justify-content:center;gap:8px}
.tbtn:hover:not(:disabled){border-color:var(--accent);color:var(--accent)}.tbtn:disabled{opacity:.45;cursor:not-allowed}
.tbtn.primary{background:var(--accent);border-color:var(--accent);color:var(--accent-ink)}.tbtn.primary:hover:not(:disabled){filter:brightness(1.08);color:var(--accent-ink)}
.tb-actions .section-entry[aria-expanded="true"]{border-color:var(--accent);color:var(--accent);background:var(--accent-soft)}
#chatDevicesLabel{font-variant-numeric:tabular-nums}
.icon-btn{padding:6px;width:36px;flex:none}.up-close,.chip-x{min-width:36px;min-height:36px}.btn{border-radius:8px;min-height:40px}
.dz:focus-within{outline:3px solid var(--accent);outline-offset:3px}.dz[aria-disabled="true"]{opacity:.6;cursor:wait}
.empty{padding:64px 24px;text-align:center;color:var(--muted);font-size:14px}.empty .icon{display:block;width:40px;height:40px;margin:0 auto 16px;opacity:.6}
.lightbox{border:0;border-radius:12px;background:#151c2a;color:#e7ecf5;padding:16px;width:min(1160px,calc(100% - 32px));max-height:calc(100vh - 32px);max-height:calc(100dvh - 32px);overflow:auto}
.lightbox[open]{display:flex;flex-direction:column;gap:16px}.lb-head{display:flex;align-items:center;gap:12px;flex:none}
.lb-name{font-size:14px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;min-width:0}.lb-pos{color:#a0adc2;font-size:12px;margin-left:auto;flex:none}
.lb-x,.lb-nav{appearance:none;border:0;width:44px;height:44px;border-radius:8px;background:rgba(255,255,255,.08);color:#fff;cursor:pointer;flex:none;display:grid;place-items:center}
.lb-x:hover,.lb-nav:hover{background:rgba(255,255,255,.18)}
.lb-body{display:grid;grid-template-columns:44px minmax(0,1fr) 44px;gap:12px;align-items:center;min-height:0}.lb-prev{grid-column:1}.lb-media{grid-column:2;grid-row:1;display:flex;align-items:center;justify-content:center;min-width:0;min-height:0}.lb-next{grid-column:3}
.lb-media img,.lb-media video{max-width:100%;max-height:calc(100vh - 164px);max-height:calc(100dvh - 164px);object-fit:contain;border-radius:8px}.lb-media audio{width:min(560px,100%)}
@media(max-width:1099px){
  .wrap{--edge:16px;gap:12px}.workspace,body.panel-open .workspace{grid-template-columns:minmax(0,1fr)}
  #chatToggle{display:none}.view-nav{display:flex;gap:4px;align-self:flex-start;padding:4px;background:var(--border);border-radius:10px;flex:none}
  .view-nav button{border:0;background:transparent;min-width:112px;min-height:36px;color:var(--muted);border-radius:7px;display:flex;align-items:center;justify-content:center;gap:8px;cursor:pointer}
  .view-nav button[aria-pressed="true"]{background:var(--card);color:var(--accent);box-shadow:var(--shadow)}
}
@media(max-width:767px){
  .wrap{--edge:12px;gap:12px}.topbar{min-height:64px;gap:8px}.title{font-size:17px;white-space:nowrap}.sub{display:none}.logo{width:36px;height:36px}
  .tb-actions .tbtn{width:44px;padding:0;gap:0}.tb-actions .action-label{position:absolute;width:1px;height:1px;overflow:hidden;clip-path:inset(50%);white-space:nowrap}
  .workspace{gap:0}.view-nav{order:3;width:100%;border-radius:12px;background:var(--card);border:1px solid var(--border);padding:4px;align-self:stretch}
  .view-nav button{flex:1;min-height:48px;min-width:0}.view-nav button[aria-pressed="true"]{background:var(--accent-soft);box-shadow:none}
  .files .head{padding:16px 12px 12px}.files h1{font-size:20px;margin-bottom:8px}.file-toolbar{padding:8px 12px;gap:4px;flex-wrap:wrap}
  .file-toolbar>.hint{font-size:12px}.tools{gap:4px}.tools .tbtn{padding:6px 8px;font-size:12px}.columns{display:none}
  .row{grid-template-columns:44px minmax(0,1fr) 44px;grid-template-rows:12px minmax(44px,auto) 12px;gap:0 8px;padding:2px 8px;min-height:72px}.row .ck{grid-column:1;grid-row:2;min-height:44px}
  .row .name{grid-column:2;grid-row:2;gap:8px;font-size:15px;line-height:20px}.row .name a{min-width:44px;min-height:44px;white-space:normal;display:flex;align-items:center;text-align:left;padding:0}
  .row .file-label{display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden;overflow-wrap:anywhere;min-width:0}
  .row .fi{width:24px;height:28px}.row .fi .icon{width:22px;height:22px}.file-meta{display:flex;grid-column:2;grid-row:3;gap:8px;flex-wrap:nowrap;padding-left:32px;min-width:0;container-type:inline-size}.row .date,.row .size{font-size:10px;font-size:clamp(8px,6cqi,10px);line-height:12px;text-align:left}.file-meta .size{order:1}.file-meta .size:empty{display:none}
  .row .dl{grid-column:3;grid-row:2;width:44px;height:44px}
  .tbtn,.btn,.up-close,.chip-x,.breadcrumbs button{min-width:44px;min-height:44px}.upload{padding:20px 16px}
  .upload-dialog{margin:auto 0 0;width:100%;max-width:100%;max-height:calc(100dvh - 16px);border-radius:16px 16px 0 0;padding-bottom:env(safe-area-inset-bottom)}
  .dz{padding:24px 12px}.dz .t2{font-size:12px}.lightbox{width:calc(100% - 16px);padding:12px}
  .lb-body{grid-template-columns:1fr 1fr;justify-items:center}.lb-media{grid-column:1/-1;grid-row:1;width:100%}.lb-prev{grid-column:1;grid-row:2}.lb-next{grid-column:2;grid-row:2}
  .lb-media img,.lb-media video{max-height:calc(100dvh - 220px)}.lightbox input,.wrap input,.wrap textarea,.upload-dialog input,#chatLimit{font-size:16px}
}
@media(max-width:1099px){body.keyboard-open .view-nav,body.keyboard-open .topbar{display:none}}
@media(pointer:coarse){.tbtn,.btn,.up-close,.chip-x,.row .dl,.row .ck,.row .name a,.breadcrumbs button,.view-nav button{min-width:44px;min-height:44px}.tool-panel input,.tool-panel .chat-compose textarea,#chatLimit{font-size:16px}}
@media(prefers-reduced-motion:reduce){*,*::before,*::after{animation:none!important;transition:none!important}}
</style>
</head>
<body>
{{template "uiIcons"}}
<div class="wrap">
  <header class="topbar">
    <div class="logo"><svg class="icon" aria-hidden="true"><use href="#i-folder"/></svg></div>
    <div class="tb-titles"><div class="title">Canyou 共享</div><div class="sub">在同一网络，轻松分享</div></div>
    <div class="tb-actions">
      {{if .CanUpload}}<button class="tbtn upload-toggle" type="button" id="uploadToggle" aria-haspopup="dialog" title="上传文件"><svg class="icon" aria-hidden="true"><use href="#i-upload"/></svg><span class="action-label" id="uploadLabel">上传文件</span></button>{{end}}
      {{if .CanChat}}<button class="tbtn section-entry" type="button" id="chatToggle" aria-controls="chatPanel" aria-expanded="false"><svg class="icon" aria-hidden="true"><use href="#i-chat"/></svg>消息<span class="badge" data-unread hidden></span></button>
      <button class="tbtn section-entry" id="chatDevicesToggle" type="button" aria-controls="devicesPanel" aria-expanded="false" title="在线设备(0)" disabled><svg class="icon" aria-hidden="true"><use href="#i-users"/></svg><span class="action-label" id="chatDevicesLabel">在线设备(0)</span></button>
      <button class="tbtn section-entry" id="chatSettingsToggle" type="button" aria-controls="settingsPanel" aria-expanded="false" title="设置"><svg class="icon" aria-hidden="true"><use href="#i-settings"/></svg><span class="action-label">设置</span></button>{{end}}
    </div>
  </header>
  {{if .CanChat}}<nav class="view-nav" aria-label="工作区导航">
    <button type="button" id="filesTab" aria-controls="filesPanel" aria-pressed="true"><svg class="icon" aria-hidden="true"><use href="#i-folder"/></svg>文件</button>
    <button type="button" id="chatTab" aria-controls="chatPanel" aria-pressed="false"><svg class="icon" aria-hidden="true"><use href="#i-chat"/></svg>消息<span class="badge" data-unread hidden></span></button>
  </nav>{{end}}
  <main class="workspace">
    <section class="card files" id="filesPanel" aria-label="共享文件" data-base="/">
      <div class="head"><h1 id="filesTitle" tabindex="-1">共享文件</h1><div class="pathbar"><button class="tbtn icon-btn" id="parentBtn" type="button" aria-label="返回上一级目录" disabled><svg class="icon" aria-hidden="true"><use href="#i-back"/></svg></button><nav class="breadcrumbs" id="pathTitle" aria-label="目录路径"></nav></div></div>
      <div class="file-toolbar"><span class="hint" id="selHint" role="status">正在加载…</span><div class="tools">
        <button class="tbtn" type="button" id="selAll">全选</button><button class="tbtn" type="button" id="selNone" disabled>取消选择</button>
        <button class="tbtn primary" type="button" id="zipBtn" disabled>下载</button>
      </div></div>
      <div class="columns" aria-hidden="true"><span></span><span>名称</span><span class="col-size">大小</span><span class="col-date">修改时间</span><span></span></div>
      <div id="fileList" aria-busy="true"><div class="empty">正在加载文件…</div></div>
    </section>
    {{template "workspacePanels" .}}
  </main>
</div>
  {{if .CanUpload}}
  <dialog class="card upload-dialog" id="uploadDialog" aria-labelledby="uploadTitle">
  <form class="upload" id="uploadPanel" method="post" action="/upload" enctype="multipart/form-data">
    <div class="up-head">
      <h2 id="uploadTitle">上传文件</h2>
      <button class="up-close" type="button" id="uploadClose" aria-label="关闭上传窗口"><svg class="icon" aria-hidden="true"><use href="#i-close"/></svg></button>
    </div>
    <label class="dz" id="dz">
      <input class="file-input" type="file" name="file" id="fileInput" multiple>
      <div class="ic"><svg class="icon" aria-hidden="true" style="width:36px;height:36px;color:var(--accent)"><use href="#i-upload"/></svg></div>
      <div class="t1">点击<b>选择文件</b>或将文件拖拽到此处</div>
      <div class="t2">支持多选 · 文件将保存到服务端的“上传目录”</div>
    </label>
    <div class="chips" id="chips"></div>
    <div class="pager" id="pager" hidden></div>
    <div class="actions">
      <span class="hint" id="hint">尚未选择文件</span>
      <button class="btn" type="submit" id="submit" disabled>上传文件</button>
    </div>
    <div class="up-result" id="uploadResult" role="status" hidden></div>
  </form>
  </dialog>
  {{end}}
{{template "identityScript"}}
{{template "themeInit"}}
{{template "workspaceScript" .}}
<script>
(function(){
  var zipBtn = document.getElementById('zipBtn');
  var list = document.getElementById('fileList');
  if (!zipBtn || !list) return;
  var card = document.querySelector('.card.files');
  var pathTitle = document.getElementById('pathTitle');
  var selHint = document.getElementById('selHint');
  var selAll = document.getElementById('selAll');
  var selNone = document.getElementById('selNone');
  var current = '/', requestedPath = '/', listRequest = 0;
  var parentBtn = document.getElementById('parentBtn');
  var lastCount = 0;
  var media = []; // 当前目录里可进浮层预览的媒体条目（{name,path,link,kind}），左右翻页用

  // 简化版 path.Clean("/"+p)：恒有首斜杠，消掉 . 与 .. 段
  function pathClean(p){
    var stack = [];
    String(p || '').split('/').forEach(function(seg){
      if (!seg || seg === '.') return;
      if (seg === '..') { stack.pop(); return; }
      stack.push(seg);
    });
    return stack.length ? '/' + stack.join('/') : '/';
  }

  // 列表动态重渲染，勾选项每次从容器内实时取
  function boxes(){ return Array.prototype.slice.call(list.querySelectorAll('.pick')); }
  function checkedItems(){
    return boxes().filter(function(b){ return b.checked; })
      .map(function(b){ return {path: b.value, dir: b.getAttribute('data-dir') === '1'}; });
  }
  function update(){
    var n = checkedItems().length;
    zipBtn.disabled = (n === 0);
    selAll.disabled = lastCount === 0 || n === lastCount;
    selNone.disabled = n === 0;
    boxes().forEach(function(b){ b.closest('.row').classList.toggle('selected', b.checked); });
    if (selHint) selHint.textContent = n ? ('已选 ' + n + ' 项') : ('共 ' + lastCount + ' 项');
  }

  // 统一的流式打包提交：base 为当前查看目录（zip 命名根），paths 为选中项完整路径；
  // POST 表单带不了 header，身份改拼在 action 的 URL 参数上
  function submitZip(paths){
    var base = card ? (card.getAttribute('data-base') || '/') : '/';
    var form = document.createElement('form');
    form.method = 'post'; form.action = window.canyouWithDid('/zip'); form.style.display = 'none';
    var b = document.createElement('input');
    b.type = 'hidden'; b.name = 'base'; b.value = base;
    form.appendChild(b);
    paths.forEach(function(p){
      var input = document.createElement('input');
      input.type = 'hidden'; input.name = 'paths'; input.value = p;
      form.appendChild(input);
    });
    document.body.appendChild(form);
    form.submit();
  }

  // 文件名是不可信数据：一律 createElement + textContent 渲染，禁止 innerHTML 拼接
  function renderBreadcrumbs(){
    pathTitle.textContent = '';
    var parts = current.split('/').filter(Boolean), target = '';
    function crumb(label, p, active){
      var b = document.createElement('button'); b.type = 'button'; b.textContent = label; b.title = p;
      if (active) b.setAttribute('aria-current', 'page');
      b.addEventListener('click', function(){ if(p !== current) navigate(p); });
      pathTitle.appendChild(b);
    }
    crumb('根目录', '/', !parts.length);
    parts.forEach(function(part, i){
      var sep = document.createElement('span'); sep.textContent = '/'; sep.setAttribute('aria-hidden', 'true'); pathTitle.appendChild(sep);
      target += '/' + part; crumb(part, target, i === parts.length - 1);
    });
    parentBtn.disabled = current === '/';
    pathTitle.scrollLeft = pathTitle.scrollWidth;
  }
  parentBtn.addEventListener('click', function(){ if(current !== '/') navigate(pathClean(current + '/..')); });
  function makeRow(item){
    var row = document.createElement('div'); row.className = 'row';
    var ck = document.createElement('label'); ck.className = 'ck';
    var box = document.createElement('input'); box.type = 'checkbox'; box.className = 'pick';
    box.value = item.path; box.setAttribute('data-dir', item.isDir ? '1' : '0');
    box.setAttribute('aria-label', '选择 ' + item.name);
    ck.appendChild(box);
    var name = document.createElement('span'); name.className = 'name';
    var fi = document.createElement('span'); fi.className = 'fi' + (item.isDir ? ' folder' : '');
    fi.appendChild(window.canyouIcon(item.isDir ? 'folder' : 'file'));
    var a = document.createElement('a'); a.href = item.link; a.title = item.name;
    var fileLabel = document.createElement('span'); fileLabel.className = 'file-label'; fileLabel.textContent = item.name; a.appendChild(fileLabel);
    if (item.isDir) a.setAttribute('data-directory', item.path);
    if (!item.isDir){
      // identityScript 只在加载时补链一次，动态生成的文件链接要自己带 did
      a.href = window.canyouWithDid(item.link);
      var kind = kindOf(item.name);
      if (kind === 'image' || kind === 'video' || kind === 'audio') { // 媒体：点击弹浮层预览而非下载，下载仍走行尾按钮
        a.className = 'preview';
        a.setAttribute('data-path', item.path);
        a.title = (kind === 'image' ? '预览 ' : '播放 ') + item.name;
      } else if (kind === 'blank') { // PDF/HTML/文本代码：新标签页，浏览器原生渲染
        a.className = 'blank';
        a.target = '_blank';
        a.rel = 'noopener';
        a.title = '新标签页打开 ' + item.name;
      } else {
        a.setAttribute('download', item.name);
      }
    }
    name.appendChild(fi); name.appendChild(a);
    var size = document.createElement('span'); size.className = 'size'; size.textContent = item.size || '';
    var date = document.createElement('span'); date.className = 'date'; date.textContent = item.modTime || '';
    var dl = document.createElement('a'); dl.appendChild(window.canyouIcon('download'));
    dl.setAttribute('aria-label', (item.isDir ? '打包下载 ' : '下载 ') + item.name);
    if (item.isDir) {
      dl.className = 'dl dlpick'; dl.href = '#';
      dl.setAttribute('data-path', item.path);
      dl.title = '打包下载 ' + item.name;
    } else {
      dl.className = 'dl';
      dl.href = window.canyouWithDid(item.link);
      dl.setAttribute('download', item.name);
      dl.title = '下载 ' + item.name;
    }
    var meta = document.createElement('span'); meta.className = 'file-meta'; meta.appendChild(size); meta.appendChild(date);
    row.appendChild(ck); row.appendChild(name); row.appendChild(meta); row.appendChild(dl);
    return row;
  }

  // ===== 媒体预览浮层：innerHTML 全为静态骨架，文件名一律 textContent 写入 =====
  // 按扩展名分流：image/video/audio 进浮层，pdf/html/文本代码开新标签，其余（doc/zip 等）仍下载
  function kindOf(name){
    if (/\.(png|jpe?g|gif|webp|bmp|ico|avif|svg)$/i.test(name)) return 'image';
    if (/\.(mp4|webm)$/i.test(name)) return 'video';
    if (/\.(mp3|wav|ogg|oga|flac|m4a|aac)$/i.test(name)) return 'audio';
    if (/\.(pdf|html?|txt|log|md|csv|json|xml|ya?ml|ini|conf|cnf|toml|properties|sql|sh|bat|ps1|py|jsx?|tsx?|go|cpp|hpp|c|h|java|rs|rb|php|css|vue|swift|kt|lua)$/i.test(name)) return 'blank';
    return '';
  }
  var lb = null, lbName = null, lbPos = null, lbIndex = -1;
  function buildLightbox(){
    lb = document.createElement('dialog'); lb.className = 'lightbox'; lb.hidden = true;
    lb.setAttribute('aria-label', '媒体预览');
    lb.innerHTML = '<div class="lb-head"><span class="lb-name"></span><span class="lb-pos"></span><button class="lb-x" type="button" aria-label="关闭预览"></button></div>' +
      '<div class="lb-body"><button class="lb-nav lb-prev" type="button" aria-label="上一项"></button><div class="lb-media"></div><button class="lb-nav lb-next" type="button" aria-label="下一项"></button></div>';
    lb.querySelector('.lb-x').appendChild(window.canyouIcon('close'));
    lb.querySelector('.lb-prev').appendChild(window.canyouIcon('prev'));
    lb.querySelector('.lb-next').appendChild(window.canyouIcon('next'));
    document.body.appendChild(lb);
    window.canyouDialog(lb);
    lb.addEventListener('cancel', function(e){ e.preventDefault(); closePreview(); });
    lb.addEventListener('close', function(){ if(lb.open) return; lb.hidden = true; releaseMedia(); lbIndex = -1; });
    lbName = lb.querySelector('.lb-name'); lbPos = lb.querySelector('.lb-pos');
    lb.addEventListener('click', function(e){
      var t = e.target;
      if (t === lb) { var r=lb.getBoundingClientRect(); if(e.clientX<r.left || e.clientX>r.right || e.clientY<r.top || e.clientY>r.bottom) closePreview(); return; } // 点媒体外围关闭
      var button = t.closest('button');
      if (!button) return;
      if (button.classList.contains('lb-x')) closePreview();
      if (button.classList.contains('lb-prev')) showPreview(lbIndex - 1);
      if (button.classList.contains('lb-next')) showPreview(lbIndex + 1);
    });
    document.addEventListener('keydown', function(e){
      if (!lb || lb.hidden) return;
      if (e.key === 'Escape') closePreview();
      else if (e.key === 'ArrowLeft') showPreview(lbIndex - 1);
      else if (e.key === 'ArrowRight') showPreview(lbIndex + 1);
    });
  }
  function releaseMedia(){
    if(!lb) return;
    var holder = lb.querySelector('.lb-media');
    holder.querySelectorAll('audio,video').forEach(function(el){
      el.pause(); el.removeAttribute('src'); el.load();
    });
    holder.textContent = '';
  }
  function setMedia(item){
    releaseMedia();
    var holder = lb.querySelector('.lb-media');
    var el;
    if (item.kind === 'video') { el = document.createElement('video'); el.controls = true; }
    else if (item.kind === 'audio') { el = document.createElement('audio'); el.controls = true; }
    else { el = document.createElement('img'); el.alt = item.name; }
    el.src = window.canyouWithDid(item.link); // 真实路径带 did，媒体本体与页面一样过门禁
    holder.appendChild(el);
  }
  function showPreview(i){
    if (i < 0 || i >= media.length) return;
    if (!lb) buildLightbox();
    lbIndex = i;
    setMedia(media[i]);
    lbName.textContent = media[i].name;
    lbPos.textContent = media.length > 1 ? ((i + 1) + ' / ' + media.length) : '';
    lb.querySelector('.lb-prev').hidden = (i === 0);
    lb.querySelector('.lb-next').hidden = (i === media.length - 1);
    lb.hidden = false;
    lb.canyouOpen();
  }
  function closePreview(){
    if (!lb) return;
    if (lb.open) lb.close();
    lb.hidden = true;
    releaseMedia();
    lbIndex = -1;
  }
  function openPreview(p){
    for (var i = 0; i < media.length; i++){ if (media[i].path === p) { showPreview(i); return; } }
  }

  function render(data){
    current = data.path;
    renderBreadcrumbs();
    if (card) card.setAttribute('data-base', current);
    var items = data.items || [];
    lastCount = items.length;
    media = [];
    items.forEach(function(it){
      if (it.isDir) return;
      var k = kindOf(it.name);
      if (k === 'image' || k === 'video' || k === 'audio') media.push({name: it.name, path: it.path, link: it.link, kind: k});
    });
    closePreview();
    list.innerHTML = '';
    items.forEach(function(item){ list.appendChild(makeRow(item)); });
    if (!items.length){
      var empty = document.createElement('div'); empty.className = 'empty';
      empty.appendChild(window.canyouIcon('folder'));
      var label = document.createElement('div'); label.textContent = '这个文件夹还是空的'; empty.appendChild(label);
      list.appendChild(empty);
    }
    list.setAttribute('aria-busy', 'false');
    update();
    list.scrollTop = 0;
  }
  function showError(msg){
    lastCount = 0;
    media = [];
    closePreview();
    list.innerHTML = '';
    var box = document.createElement('div'); box.className = 'empty';
    var tip = document.createElement('div'); tip.textContent = msg;
    box.appendChild(tip);
    var retry = document.createElement('button'); retry.type = 'button'; retry.className = 'tbtn'; retry.style.marginTop = '16px'; retry.textContent = '重新加载';
    retry.addEventListener('click', function(){ show(requestedPath); }); box.appendChild(retry);
    if (requestedPath !== '/'){
      var btn = document.createElement('button'); btn.type = 'button'; btn.className = 'tbtn'; btn.style.marginTop = '12px';
      btn.textContent = '返回根目录';
      btn.addEventListener('click', function(){ navigate('/'); });
      box.appendChild(btn);
    }
    list.appendChild(box);
    list.setAttribute('aria-busy', 'false');
    update();
  }

  // 拉取并渲染目录；不碰历史栈，供初始化/前进后退/错误重试复用
  function show(p){
    requestedPath = p;
    var requestId = ++listRequest;
    closePreview();
    var pending = document.createElement('div'); pending.className = 'empty'; pending.textContent = '正在加载文件…';
    list.innerHTML = ''; list.appendChild(pending); list.setAttribute('aria-busy', 'true');
    lastCount = 0; update(); selHint.textContent = '正在加载…';
    fetch('/api/list', {method:'post', cache:'no-store',
      headers:{'Content-Type':'application/json','X-Device-ID':window.CANYOU_DID||''},
      body:JSON.stringify({path:p})})
      .then(function(resp){
        if (resp.status === 403) return {ok:false, message:'该设备尚未通过访问验证'};
        return resp.json().catch(function(){ return {ok:false, message:'服务器响应异常'}; });
      })
      .then(function(data){
        if (requestId !== listRequest) return;
        if (!data || !data.ok) { showError((data && data.message) || '加载失败'); return; }
        render(data);
        if (window.canyouUI) canyouUI({dir: data.path === '/' ? '' : data.path}); // 记住最近浏览目录，刷新后还原
      })
      .catch(function(){ if(requestId === listRequest) showError('加载失败：网络错误'); });
  }
  // 目录切换：pushState 不改 URL（恒为 /）只增历史栈，后退/前进可用
  function navigate(p){
    p = pathClean(p);
    if (window.history && history.pushState) history.pushState({p:p}, '', '/');
    show(p);
  }

  // 事件委托：动态渲染的行都在 #fileList 内，容器上只绑一次
  list.addEventListener('click', function(e){
    var el = e.target.closest ? e.target.closest('a') : null;
    if (!el) return;
    if (el.classList.contains('dlpick')) { e.preventDefault(); submitZip([el.getAttribute('data-path')]); return; } // 文件夹打包下载
    if (el.classList.contains('dl')) return; // 文件下载走真实路径，保持浏览器原生行为
    if (el.classList.contains('preview')) { e.preventDefault(); openPreview(el.getAttribute('data-path')); return; } // 媒体名：点击弹层预览
    var directory = el.getAttribute('data-directory');
    if (directory !== null) { e.preventDefault(); navigate(directory); return; }
    var parent = el.getAttribute('data-parent');
    if (parent !== null) { e.preventDefault(); navigate(parent); return; } // .. 行：回父目录
    var href = el.getAttribute('href') || '';
    if (href.charAt(0) === '#' || href.indexOf('?') !== -1) return; // 带 did 的下载链接不拦截
    e.preventDefault(); // 目录行：不再整页跳转，改走列表接口
    navigate(href);
  });
  list.addEventListener('change', function(e){ if (e.target.classList && e.target.classList.contains('pick')) update(); });
  selAll.addEventListener('click', function(){ boxes().forEach(function(b){ b.checked = true; }); update(); });
  selNone.addEventListener('click', function(){ boxes().forEach(function(b){ b.checked = false; }); update(); });
  zipBtn.addEventListener('click', function(){
    var items = checkedItems();
    if (!items.length) return;
    if (items.length === 1 && !items[0].dir) { // 仅选中单个文件时直接下载原文件
      var a = document.createElement('a');
      a.href = window.canyouWithDid(items[0].path); a.download = items[0].path.split('/').pop();
      document.body.appendChild(a); a.click(); a.remove();
      return;
    }
    submitZip(items.map(function(i){ return i.path; })); // 含文件夹或多项：打包
  });
  window.addEventListener('popstate', function(e){ show((e.state && e.state.p) || '/'); });

  // 初始化：旧目录 URL 作为初始目录后立即把地址栏清成 /；直接访根页时还原上次浏览的目录
  // location.pathname 是非 ASCII 的百分号编码串，单独还原；localStorage 里的目录本就是原文，不能再解
  function decodePath(p){
    try { return decodeURIComponent(String(p || '')); } catch (e) { return String(p || ''); } // 非法编码片段按原文处理
  }
  var initial = pathClean(decodePath(location.pathname));
  if (initial === '/' && window.canyouReadUI){
    var saved = canyouReadUI().dir;
    if (saved) initial = pathClean(saved);
  }
  if (window.history && history.replaceState) history.replaceState({p:initial}, '', '/');
  show(initial);
})();
</script>

<script>
(function(){
  var toggle = document.getElementById('uploadToggle');
  var panel = document.getElementById('uploadDialog');
  var closeBtn = document.getElementById('uploadClose');
  if (!toggle || !panel) return;
  window.canyouDialog(panel);
  function open(){ panel.canyouOpen(); window.canyouUI({upload:true}); }
  panel.addEventListener('close', function(){ if(!panel.open) window.canyouUI({upload:false}); });
  toggle.addEventListener('click', open);
  closeBtn.addEventListener('click', function(){ panel.close(); });
  panel.addEventListener('click', function(e){
    if(e.target !== panel) return;
    var r=panel.getBoundingClientRect();
    if(e.clientX<r.left || e.clientX>r.right || e.clientY<r.top || e.clientY>r.bottom) panel.close();
  });
  // 刷新只还原面板开合，浏览器不会恢复待上传文件。
  if (window.canyouReadUI().upload) open();
})();
</script>

{{if .CanUpload}}
<script>
(function(){
  var input = document.getElementById('fileInput');
  var dz = document.getElementById('dz');
  var chips = document.getElementById('chips');
  var pager = document.getElementById('pager');
  var submit = document.getElementById('submit');
  var hint = document.getElementById('hint');
  var form = document.getElementById('uploadPanel');
  var resultBox = document.getElementById('uploadResult');
  if (!input) return;

  var PAGE_SIZE = 5; // 每页固定条数，避免文件过多时窗口不断向上撑高
  var page = 0;
  var selected = []; // 已选文件；支持逐个移除，提交前同步回 input.files
  var uploading = false;
  function setUploading(value){
    uploading = value; input.disabled = value;
    dz.setAttribute('aria-disabled', String(value)); form.setAttribute('aria-busy', String(value));
    document.getElementById('uploadLabel').textContent = value ? '上传中…' : '上传文件';
    submit.textContent = value ? '上传中…' : '上传文件';
    render();
  }

  function fmt(b){
    if (b < 1024) return b + ' B';
    var u = ['KB','MB','GB','TB','PB'], i = -1;
    do { b /= 1024; i++; } while (b >= 1024 && i < u.length - 1);
    return b.toFixed(1) + ' ' + u[i];
  }
  function totalPages(){ return Math.max(1, Math.ceil(selected.length / PAGE_SIZE)); }
  function syncInput(){ // 把 selected 重建到 input.files，保证原生表单提交只携带未移除的文件
    var dt = new DataTransfer();
    selected.forEach(function(f){ dt.items.add(f); });
    input.files = dt.files;
  }
  function removeAt(index){
    if (uploading) return;
    selected.splice(index, 1);
    hideResult();
    syncInput();
    render();
  }
  function addFiles(fileList){
    if (uploading) return;
    hideResult();
    var before = selected.length;
    Array.prototype.forEach.call(fileList, function(f){
      var dup = selected.some(function(s){ return s.name === f.name && s.size === f.size; });
      if (!dup) selected.push(f);
    });
    if (selected.length !== before) page = totalPages() - 1; // 有新文件则跳到最后一页
    syncInput();
    render();
  }
  function renderPager(){
    var tp = totalPages();
    if (selected.length <= PAGE_SIZE){ pager.hidden = true; pager.innerHTML = ''; return; }
    pager.hidden = false;
    pager.innerHTML = '';
    var prev = document.createElement('button'); prev.type = 'button'; prev.className = 'tbtn'; prev.textContent = '‹ 上一页'; prev.disabled = (page === 0);
    prev.addEventListener('click', function(){ if (page > 0){ page--; render(); } });
    var info = document.createElement('span'); info.className = 'pager-info'; info.textContent = (page + 1) + ' / ' + tp;
    var next = document.createElement('button'); next.type = 'button'; next.className = 'tbtn'; next.textContent = '下一页 ›'; next.disabled = (page >= tp - 1);
    next.addEventListener('click', function(){ if (page < tp - 1){ page++; render(); } });
    pager.appendChild(prev); pager.appendChild(info); pager.appendChild(next);
  }
  function render(){
    chips.innerHTML = '';
    var tp = totalPages();
    if (page > tp - 1) page = tp - 1;
    if (page < 0) page = 0;
    var start = page * PAGE_SIZE;
    var end = Math.min(start + PAGE_SIZE, selected.length);
    for (var i = start; i < end; i++){
      var f = selected[i];
      var chip = document.createElement('span'); chip.className = 'chip';
      var nm = document.createElement('span'); nm.className = 'nm'; nm.textContent = f.name; nm.title = f.name;
      var sz = document.createElement('span'); sz.className = 'sz'; sz.textContent = fmt(f.size);
      var rm = document.createElement('button'); rm.type = 'button'; rm.className = 'chip-x'; rm.appendChild(window.canyouIcon('close')); rm.setAttribute('aria-label', '移除 ' + f.name);
      (function(index){ rm.addEventListener('click', function(){ removeAt(index); }); })(i);
      rm.disabled = uploading;
      chip.appendChild(nm); chip.appendChild(sz); chip.appendChild(rm);
      chips.appendChild(chip);
    }
    renderPager();
    var n = selected.length;
    submit.disabled = uploading || (n === 0);
    hint.textContent = uploading ? '正在上传，请勿关闭网页' : (n ? ('已选择 ' + n + ' 个文件') : '尚未选择文件');
  }
  input.addEventListener('change', function(){ addFiles(input.files); });
  ['dragenter','dragover'].forEach(function(ev){
    dz.addEventListener(ev, function(e){ e.preventDefault(); dz.classList.add('over'); });
  });
  ['dragleave','dragend'].forEach(function(ev){
    dz.addEventListener(ev, function(){ dz.classList.remove('over'); });
  });
  dz.addEventListener('drop', function(e){
    e.preventDefault(); dz.classList.remove('over');
    if (e.dataTransfer && e.dataTransfer.files.length) addFiles(e.dataTransfer.files);
  });

  function clearSelected(){
    selected = [];
    page = 0;
    syncInput();
    render();
  }
  function hideResult(){
    if (!resultBox) return;
    resultBox.hidden = true;
    resultBox.textContent = '';
    resultBox.className = 'up-result';
  }
  function showResult(data){
    if (!resultBox) return;
    resultBox.textContent = '';
    var failed = data.failed || [];
    var partial = data.ok && failed.length > 0;
    resultBox.className = 'up-result ' + (partial ? 'partial' : (data.ok ? 'ok' : 'bad'));
    resultBox.hidden = false;
    var head = document.createElement('div');
    head.textContent = data.message || (partial ? '部分文件上传失败' : (data.ok ? '上传成功' : '上传失败'));
    resultBox.appendChild(head);
    if (failed.length){
      var ul = document.createElement('ul');
      failed.forEach(function(item){ var li = document.createElement('li'); li.textContent = item; ul.appendChild(li); });
      resultBox.appendChild(ul);
    }
  }
  // 就地上传：fetch 提交后在悬浮窗内展示结果，全部成功则清空列表，不再跳转独立结果页
  if (form) form.addEventListener('submit', function(e){
    e.preventDefault();
    if (uploading || !selected.length) return;
    hideResult();
    var payload = new FormData(form);
    setUploading(true);
    fetch('/upload', { method: 'post', body: payload, headers: {'X-Device-ID': window.CANYOU_DID || ''} })
      .then(function(resp){ return resp.json().catch(function(){ return {ok:false, message:'服务器响应异常'}; }); })
      .then(function(data){
        showResult(data);
        if (data.ok && !(data.failed && data.failed.length)) clearSelected();
      })
      .catch(function(){ showResult({ok:false, message:'上传失败：网络错误'}); })
      .then(function(){ setUploading(false); });
  });
})();
</script>
{{end}}
{{template "chatScript" .}}
</body>
</html>`
