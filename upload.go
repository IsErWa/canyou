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

// listingData 目录浏览页模板数据
type listingData struct {
	Title      string
	CanUpload  bool
	ParentLink string
	Items      []listingItem
}

// listingItem 目录浏览页中的单个条目
type listingItem struct {
	Name        string
	Link        string
	Path        string
	Icon        string
	Size        string
	ModTime     string
	IsDirectory bool
}

// uploadResult 上传结果，以 JSON 返回给前端就地展示
type uploadResult struct {
	OK      bool     `json:"ok"`
	Message string   `json:"message"`
	Saved   []string `json:"saved"`
	Failed  []string `json:"failed"`
}

var listingTemplate = template.Must(template.New("listing").Parse(listingHTML))

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

// serveDirectoryListing 渲染带上传按钮的目录浏览页
func serveDirectoryListing(writer http.ResponseWriter, request *http.Request, viewDir, uploadDir string) {
	reqPath := path.Clean("/" + request.URL.Path)
	fullDir := filepath.Join(viewDir, filepath.FromSlash(reqPath))

	entries, err := os.ReadDir(fullDir)
	if err != nil {
		http.Error(writer, "读取目录失败", http.StatusInternalServerError)
		return
	}

	data := listingData{Title: reqPath, CanUpload: uploadDir != ""}
	if reqPath != "/" {
		data.ParentLink = path.Dir(reqPath)
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

	for _, item := range sortable {
		var size, modTime string
		modTime = item.info.ModTime().Format("2006-01-02 15:04")
		if !item.info.IsDir() {
			size = humanSize(item.info.Size())
		}
		icon := "📄"
		if item.info.IsDir() {
			icon = "📁"
		}
		itemPath := joinURL(reqPath, item.info.Name(), false)
		data.Items = append(data.Items, listingItem{
			Name: item.info.Name(), Link: joinURL(reqPath, item.info.Name(), item.info.IsDir()),
			Path: itemPath,
			Icon: icon, Size: size, ModTime: modTime, IsDirectory: item.info.IsDir(),
		})
	}

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
<meta name="theme-color" content="#4f6ef7">
<title>Canyou · {{.Title}}</title>
<style>
:root{
  --bg:#f6f7fb; --card:#ffffff; --text:#1f2430; --muted:#8a90a2;
  --border:#e8eaf0; --accent:#4f6ef7; --accent-ink:#ffffff; --accent-soft:#eef1fe;
  --ok:#12a150; --bad:#e5484d;
  --shadow:0 1px 2px rgba(20,25,40,.04), 0 10px 28px rgba(20,25,40,.06);
  --topbar-h:66px;
}
@media (prefers-color-scheme: dark){
  :root{
    --bg:#0e1116; --card:#161b23; --text:#e6e9f0; --muted:#8b93a7;
    --border:#232a35; --accent:#6d8bff; --accent-ink:#0b1020; --accent-soft:#1b2340;
    --ok:#3ddc84; --bad:#ff6b6b;
    --shadow:0 1px 2px rgba(0,0,0,.3), 0 12px 32px rgba(0,0,0,.35);
  }
}
*{box-sizing:border-box}
body{margin:0;background:var(--bg);color:var(--text);line-height:1.5;-webkit-font-smoothing:antialiased;
  font-family:-apple-system,BlinkMacSystemFont,"Segoe UI","Microsoft YaHei UI",Roboto,Helvetica,Arial,sans-serif}
a{color:inherit;text-decoration:none}
.wrap{max-width:920px;margin:0 auto;padding:20px 16px 56px}
.topbar{display:flex;align-items:center;gap:12px;padding:6px 2px 18px;position:sticky;top:0;z-index:20;background:var(--bg)}
.topbar .tb-titles{min-width:0}
.topbar .upload-toggle{margin-left:auto;flex:none;padding:8px 18px;font-size:13px}
.logo{width:42px;height:42px;border-radius:13px;flex:none;display:grid;place-items:center;font-size:21px;
  background:linear-gradient(135deg,var(--accent),#9a6bff);box-shadow:var(--shadow)}
.title{font-size:18px;font-weight:650}
.sub{font-size:12.5px;color:var(--muted);word-break:break-all}
.card{background:var(--card);border:1px solid var(--border);border-radius:16px;box-shadow:var(--shadow)}
.upload{position:fixed;right:18px;bottom:calc(18px + env(safe-area-inset-bottom));z-index:50;width:min(360px,calc(100vw - 36px));padding:16px;
  max-height:calc(100vh - 36px);overflow:auto;
  box-shadow:0 14px 44px rgba(20,25,40,.28);opacity:0;visibility:hidden;transform:translateY(14px) scale(.98);
  transition:opacity .22s ease, transform .22s ease, visibility .22s}
.upload.open{opacity:1;visibility:visible;transform:none}
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
.files .head{display:flex;justify-content:space-between;align-items:center;padding:15px 18px;border-bottom:1px solid var(--border);
  position:sticky;top:var(--topbar-h);z-index:10;background:var(--card);border-radius:16px 16px 0 0}
.files > :last-child{border-bottom-left-radius:16px;border-bottom-right-radius:16px}
.files .head h2{font-size:14px;margin:0;font-weight:650}
.row{display:grid;grid-template-columns:24px 1fr 90px 150px 56px;align-items:center;gap:14px;padding:12px 18px;border-bottom:1px solid var(--border);transition:.12s}
.row:last-child{border-bottom:0}
a.row:hover,.row:hover{background:var(--bg)}
.row .ck{display:flex;align-items:center;justify-content:center}
.row input[type=checkbox]{width:16px;height:16px;accent-color:var(--accent);cursor:pointer;margin:0}
.row .name{display:flex;align-items:center;gap:11px;min-width:0;font-weight:500}
.row .fi{font-size:19px;flex:none}
.row .name a,.row .name span{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.row .size{color:var(--muted);font-size:13px;white-space:nowrap;font-variant-numeric:tabular-nums;text-align:right}
.row .date{color:var(--muted);font-size:13px;white-space:nowrap;text-align:right}
.row .dl{justify-self:end;padding:4px 13px;border:1px solid var(--border);border-radius:999px;font-size:12px;font-weight:550;
  line-height:1.6;color:var(--accent);background:var(--card);transition:.15s;white-space:nowrap}
.row .dl:hover{background:var(--accent);border-color:var(--accent);color:var(--accent-ink);box-shadow:var(--shadow)}
.tools{display:flex;align-items:center;gap:8px}
.tbtn{appearance:none;border:1px solid var(--border);border-radius:999px;padding:5px 13px;font-size:12px;font-weight:550;cursor:pointer;
  background:var(--card);color:var(--text);transition:.15s;font-family:inherit;white-space:nowrap}
.tbtn:hover{border-color:var(--accent);color:var(--accent)}
.tbtn:disabled{opacity:.45;cursor:not-allowed}
.tbtn.primary{background:var(--accent);border-color:var(--accent);color:var(--accent-ink)}
.tbtn.primary:hover:not(:disabled){filter:brightness(1.08);color:var(--accent-ink)}
@media (max-width:600px){.tools .hint{display:none}}
.empty{padding:40px 18px;text-align:center;color:var(--muted);font-size:14px}
@media (max-width:600px){
  .wrap{padding:14px 12px 40px}
  .row{display:flex;gap:10px;padding:13px 15px}
  .row .name{flex:1;min-width:0}
  .row .date{display:none}
  .row .dl{justify-self:auto}
  .dz{padding:24px 14px}
  .upload{left:0;right:0;bottom:0;width:100%;border-radius:16px 16px 0 0;max-height:calc(100vh - 24px);
    padding-bottom:calc(16px + env(safe-area-inset-bottom))}
}
</style>
</head>
<body>
<div class="wrap">
  <div class="topbar">
    <div class="logo">📁</div>
    <div class="tb-titles">
      <div class="title">Canyou 共享</div>
      <div class="sub">{{.Title}}</div>
    </div>
    {{if .CanUpload}}<button class="tbtn primary upload-toggle" type="button" id="uploadToggle">上传</button>{{end}}
  </div>

  {{if .CanUpload}}
  <form class="card upload" id="uploadPanel" method="post" action="/upload" enctype="multipart/form-data" aria-hidden="true">
    <div class="up-head">
      <h2>上传文件</h2>
      <button class="up-close" type="button" id="uploadClose" aria-label="关闭上传窗口">✕</button>
    </div>
    <label class="dz" id="dz">
      <input class="file-input" type="file" name="file" id="fileInput" multiple>
      <div class="ic">⬆️</div>
      <div class="t1">点击<b>选择文件</b>或将文件拖拽到此处</div>
      <div class="t2">支持多选 · 文件将保存到服务端的“上传目录”</div>
    </label>
    <div class="chips" id="chips"></div>
    <div class="pager" id="pager" hidden></div>
    <div class="actions">
      <span class="hint" id="hint">尚未选择文件</span>
      <button class="btn" type="submit" id="submit" disabled>上传文件</button>
    </div>
    <div class="up-result" id="uploadResult" hidden></div>
  </form>
  {{end}}

  <div class="card files" data-base="{{.Title}}">
    <div class="head">
      <h2>文件</h2>
      <div class="tools">
        <button class="tbtn" type="button" id="selAll">全选</button>
        <button class="tbtn" type="button" id="selNone">取消全选</button>
        <span class="hint" id="selHint">共 {{len .Items}} 项</span>
        <button class="tbtn primary" type="button" id="zipBtn" disabled>下载</button>
      </div>
    </div>
    {{if .ParentLink}}
    <a class="row" href="{{.ParentLink}}">
      <span class="ck"></span>
      <span class="name"><span class="fi">📁</span><span>..</span></span>
      <span class="size"></span><span class="date"></span>
    </a>
    {{end}}
    {{range .Items}}
    <div class="row">
      <span class="ck"><input type="checkbox" class="pick" value="{{.Path}}" data-dir="{{if .IsDirectory}}1{{else}}0{{end}}" aria-label="选择 {{.Name}}"></span>
      <span class="name"><span class="fi">{{.Icon}}</span><a href="{{.Link}}">{{.Name}}</a></span>
      <span class="size">{{.Size}}</span>
      <span class="date">{{.ModTime}}</span>
      {{if .IsDirectory}}<a class="dl dlpick" href="#" data-path="{{.Path}}" title="打包下载 {{.Name}}">下载</a>{{else}}<a class="dl" href="{{.Link}}" download="{{.Name}}" title="下载 {{.Name}}">下载</a>{{end}}
    </div>
    {{else}}
    {{if not .ParentLink}}<div class="empty">此目录暂无文件</div>{{end}}
    {{end}}
  </div>
</div>

<script>
(function(){
  var card = document.querySelector('.card.files');
  var zipBtn = document.getElementById('zipBtn');
  if (!zipBtn) return;
  var base = card ? (card.getAttribute('data-base') || '/') : '/';

  // 统一的流式打包提交：base 为当前查看目录（zip 命名根），paths 为选中项完整路径
  function submitZip(paths){
    var form = document.createElement('form');
    form.method = 'post'; form.action = '/zip'; form.style.display = 'none';
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

  // 文件夹行：点击“下载”即打包该文件夹（与批量下载同一 POST 逻辑）
  Array.prototype.forEach.call(document.querySelectorAll('.dlpick'), function(el){
    el.addEventListener('click', function(e){
      e.preventDefault();
      submitZip([el.getAttribute('data-path')]);
    });
  });

  // 批量选择下载
  var boxes = Array.prototype.slice.call(document.querySelectorAll('.pick'));
  var selAll = document.getElementById('selAll');
  var selNone = document.getElementById('selNone');
  var selHint = document.getElementById('selHint');
  var baseHint = selHint ? selHint.textContent : '';
  function checkedItems(){
    return boxes.filter(function(b){ return b.checked; })
      .map(function(b){ return {path: b.value, dir: b.getAttribute('data-dir') === '1'}; });
  }
  function update(){
    var n = checkedItems().length;
    zipBtn.disabled = (n === 0);
    if (selHint) selHint.textContent = n ? ('已选 ' + n + ' 项') : baseHint;
  }
  boxes.forEach(function(b){ b.addEventListener('change', update); });
  selAll.addEventListener('click', function(){ boxes.forEach(function(b){ b.checked = true; }); update(); });
  selNone.addEventListener('click', function(){ boxes.forEach(function(b){ b.checked = false; }); update(); });
  zipBtn.addEventListener('click', function(){
    var items = checkedItems();
    if (!items.length) return;
    if (items.length === 1 && !items[0].dir) { // 仅选中单个文件时直接下载原文件
      var a = document.createElement('a');
      a.href = items[0].path; a.download = items[0].path.split('/').pop();
      document.body.appendChild(a); a.click(); a.remove();
      return;
    }
    submitZip(items.map(function(i){ return i.path; })); // 含文件夹或多项：打包
  });
})();
</script>

<script>
(function(){
  var root = document.documentElement;
  var topbar = document.querySelector('.topbar');
  function syncTopbarH(){ if (topbar) root.style.setProperty('--topbar-h', topbar.offsetHeight + 'px'); }
  syncTopbarH();
  window.addEventListener('resize', syncTopbarH);

  var toggle = document.getElementById('uploadToggle');
  var panel = document.getElementById('uploadPanel');
  var closeBtn = document.getElementById('uploadClose');
  if (!toggle || !panel) return;
  function setOpen(open){
    panel.classList.toggle('open', open);
    panel.setAttribute('aria-hidden', open ? 'false' : 'true');
    toggle.textContent = open ? '收起' : '上传';
  }
  // 上传为右下角悬浮窗，展开不影响列表布局与滚动位置，无需回顶或随滚动收起
  toggle.addEventListener('click', function(){ setOpen(!panel.classList.contains('open')); });
  if (closeBtn) closeBtn.addEventListener('click', function(){ setOpen(false); toggle.focus(); });
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
    selected.splice(index, 1);
    hideResult();
    syncInput();
    render();
  }
  function addFiles(fileList){
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
      var rm = document.createElement('button'); rm.type = 'button'; rm.className = 'chip-x'; rm.textContent = '✕'; rm.setAttribute('aria-label', '移除 ' + f.name);
      (function(index){ rm.addEventListener('click', function(){ removeAt(index); }); })(i);
      chip.appendChild(nm); chip.appendChild(sz); chip.appendChild(rm);
      chips.appendChild(chip);
    }
    renderPager();
    var n = selected.length;
    submit.disabled = (n === 0);
    hint.textContent = n ? ('已选择 ' + n + ' 个文件') : '尚未选择文件';
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
    head.textContent = (partial ? '⚠️ ' : (data.ok ? '✅ ' : '❌ ')) + (data.message || (data.ok ? '上传成功' : '上传失败'));
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
    if (!selected.length) return;
    hideResult();
    submit.disabled = true;
    submit.textContent = '上传中…';
    fetch('/upload', { method: 'post', body: new FormData(form) })
      .then(function(resp){ return resp.json().catch(function(){ return {ok:false, message:'服务器响应异常'}; }); })
      .then(function(data){
        showResult(data);
        if (data.ok && !(data.failed && data.failed.length)) clearSelected();
      })
      .catch(function(){ showResult({ok:false, message:'上传失败：网络错误'}); })
      .then(function(){ submit.textContent = '上传文件'; submit.disabled = (selected.length === 0); });
  });
})();
</script>
{{end}}
</body>
</html>`
