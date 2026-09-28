package main

import (
	"fmt"
	"html/template"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path"
	"path/filepath"
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
	Name    string
	Link    string
	Icon    string
	Size    string
	ModTime string
}

// resultData 上传结果页模板数据
type resultData struct {
	Message  string
	Saved    []string
	Failed   []string
	BackLink string
}

var (
	listingTemplate = template.Must(template.New("listing").Parse(listingHTML))
	resultTemplate  = template.Must(template.New("result").Parse(resultHTML))
)

// isDirectoryPath 判断请求路径映射到 viewDir 下是否为目录
func isDirectoryPath(viewDir, urlPath string) bool {
	name := filepath.FromSlash(path.Clean("/" + urlPath))
	info, err := os.Stat(filepath.Join(viewDir, name))
	return err == nil && info.IsDir()
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
	for _, entry := range entries {
		var size, modTime string
		if info, err := entry.Info(); err == nil {
			modTime = info.ModTime().Format("2006-01-02 15:04")
			if !entry.IsDir() {
				size = humanSize(info.Size())
			}
		}
		icon := "📄"
		if entry.IsDir() {
			icon = "📁"
		}
		data.Items = append(data.Items, listingItem{
			Name: entry.Name(), Link: joinURL(reqPath, entry.Name(), entry.IsDir()),
			Icon: icon, Size: size, ModTime: modTime,
		})
	}

	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := listingTemplate.Execute(writer, data); err != nil {
		logServiceFailure(err) // 响应已部分写出，仅记录
	}
}

// handleFileUpload 接收 multipart 上传，将文件保存到 uploadDir
func handleFileUpload(writer http.ResponseWriter, request *http.Request, uploadDir string) {
	if request.Method != http.MethodPost {
		http.Error(writer, "上传仅支持 POST", http.StatusMethodNotAllowed)
		return
	}
	if uploadDir == "" {
		http.Error(writer, "服务端未配置上传目录", http.StatusBadRequest)
		return
	}
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		http.Error(writer, "上传目录不可用", http.StatusInternalServerError)
		return
	}

	request.Body = http.MaxBytesReader(writer, request.Body, maxUploadBytes)
	if err := request.ParseMultipartForm(uploadMemoryBuffer); err != nil {
		http.Error(writer, "上传解析失败或文件过大", http.StatusRequestEntityTooLarge)
		return
	}
	defer request.MultipartForm.RemoveAll()

	result := resultData{BackLink: "/"}
	headers := request.MultipartForm.File["file"]
	if len(headers) == 0 {
		result.Message = "未选择任何文件"
		renderResult(writer, result)
		return
	}

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
		result.Message = "上传成功"
	case len(result.Saved) > 0:
		result.Message = "部分文件上传成功"
	default:
		result.Message = "上传失败"
	}
	renderResult(writer, result)
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

// renderResult 渲染上传结果页
func renderResult(writer http.ResponseWriter, data resultData) {
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := resultTemplate.Execute(writer, data); err != nil {
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
  --shadow:0 1px 2px rgba(20,25,40,.04), 0 10px 28px rgba(20,25,40,.06);
}
@media (prefers-color-scheme: dark){
  :root{
    --bg:#0e1116; --card:#161b23; --text:#e6e9f0; --muted:#8b93a7;
    --border:#232a35; --accent:#6d8bff; --accent-ink:#0b1020; --accent-soft:#1b2340;
    --shadow:0 1px 2px rgba(0,0,0,.3), 0 12px 32px rgba(0,0,0,.35);
  }
}
*{box-sizing:border-box}
body{margin:0;background:var(--bg);color:var(--text);line-height:1.5;-webkit-font-smoothing:antialiased;
  font-family:-apple-system,BlinkMacSystemFont,"Segoe UI","Microsoft YaHei UI",Roboto,Helvetica,Arial,sans-serif}
a{color:inherit;text-decoration:none}
.wrap{max-width:920px;margin:0 auto;padding:20px 16px 56px}
.topbar{display:flex;align-items:center;gap:12px;padding:6px 2px 18px}
.logo{width:42px;height:42px;border-radius:13px;flex:none;display:grid;place-items:center;font-size:21px;
  background:linear-gradient(135deg,var(--accent),#9a6bff);box-shadow:var(--shadow)}
.title{font-size:18px;font-weight:650}
.sub{font-size:12.5px;color:var(--muted);word-break:break-all}
.card{background:var(--card);border:1px solid var(--border);border-radius:16px;box-shadow:var(--shadow)}
.upload{padding:18px;margin-bottom:18px}
.dz{position:relative;display:flex;flex-direction:column;align-items:center;justify-content:center;gap:6px;text-align:center;
  padding:30px 18px;border:1.6px dashed var(--border);border-radius:14px;cursor:pointer;transition:.15s;background:var(--bg)}
.dz:hover,.dz.over{border-color:var(--accent);background:var(--accent-soft)}
.dz .ic{font-size:28px}
.dz .t1{font-weight:600}
.dz .t1 b{color:var(--accent)}
.dz .t2{font-size:12.5px;color:var(--muted)}
.file-input{position:absolute;width:1px;height:1px;opacity:0;clip:rect(0 0 0 0);overflow:hidden}
.chips{display:flex;flex-wrap:wrap;gap:8px;margin-top:14px}
.chip{display:inline-flex;align-items:center;gap:8px;max-width:100%;padding:6px 12px;border-radius:999px;font-size:12.5px;
  background:var(--card);border:1px solid var(--border);box-shadow:var(--shadow)}
.chip .nm{overflow:hidden;text-overflow:ellipsis;white-space:nowrap;max-width:200px}
.chip .sz{color:var(--muted)}
.actions{display:flex;align-items:center;gap:14px;margin-top:16px;flex-wrap:wrap}
.btn{appearance:none;border:0;border-radius:12px;padding:11px 22px;font-size:14px;font-weight:650;cursor:pointer;
  background:var(--accent);color:var(--accent-ink);transition:.15s;box-shadow:var(--shadow)}
.btn:hover{filter:brightness(1.06)}
.btn:disabled{opacity:.45;cursor:not-allowed;box-shadow:none}
.hint{font-size:12.5px;color:var(--muted)}
.files{overflow:hidden}
.files .head{display:flex;justify-content:space-between;align-items:center;padding:15px 18px;border-bottom:1px solid var(--border)}
.files .head h2{font-size:14px;margin:0;font-weight:650}
.row{display:grid;grid-template-columns:1fr auto 150px;align-items:center;gap:14px;padding:12px 18px;border-bottom:1px solid var(--border);transition:.12s}
.row:last-child{border-bottom:0}
a.row:hover,.row:hover{background:var(--bg)}
.row .name{display:flex;align-items:center;gap:11px;min-width:0;font-weight:500}
.row .fi{font-size:19px;flex:none}
.row .name a,.row .name span{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.row .size{color:var(--muted);font-size:13px;white-space:nowrap;font-variant-numeric:tabular-nums;text-align:right}
.row .date{color:var(--muted);font-size:13px;white-space:nowrap;text-align:right}
.empty{padding:40px 18px;text-align:center;color:var(--muted);font-size:14px}
@media (max-width:600px){
  .wrap{padding:14px 12px 40px}
  .row{grid-template-columns:1fr auto;gap:10px;padding:13px 15px}
  .row .date{display:none}
  .upload{padding:14px}
  .dz{padding:24px 14px}
}
</style>
</head>
<body>
<div class="wrap">
  <div class="topbar">
    <div class="logo">📁</div>
    <div>
      <div class="title">Canyou 共享</div>
      <div class="sub">{{.Title}}</div>
    </div>
  </div>

  {{if .CanUpload}}
  <form class="card upload" method="post" action="/upload" enctype="multipart/form-data">
    <label class="dz" id="dz">
      <input class="file-input" type="file" name="file" id="fileInput" multiple>
      <div class="ic">⬆️</div>
      <div class="t1">点击<b>选择文件</b>或将文件拖拽到此处</div>
      <div class="t2">支持多选 · 文件将保存到服务端的“上传目录”</div>
    </label>
    <div class="chips" id="chips"></div>
    <div class="actions">
      <button class="btn" type="submit" id="submit" disabled>上传文件</button>
      <span class="hint" id="hint">尚未选择文件</span>
    </div>
  </form>
  {{else}}
  <div class="card upload"><span class="hint">服务端未配置上传目录，上传功能当前不可用。</span></div>
  {{end}}

  <div class="card files">
    <div class="head"><h2>文件</h2><span class="hint">共 {{len .Items}} 项</span></div>
    {{if .ParentLink}}
    <a class="row" href="{{.ParentLink}}">
      <span class="name"><span class="fi">📁</span><span>..</span></span>
      <span class="size"></span><span class="date"></span>
    </a>
    {{end}}
    {{range .Items}}
    <div class="row">
      <span class="name"><span class="fi">{{.Icon}}</span><a href="{{.Link}}">{{.Name}}</a></span>
      <span class="size">{{.Size}}</span>
      <span class="date">{{.ModTime}}</span>
    </div>
    {{else}}
    {{if not .ParentLink}}<div class="empty">此目录暂无文件</div>{{end}}
    {{end}}
  </div>
</div>

{{if .CanUpload}}
<script>
(function(){
  var input = document.getElementById('fileInput');
  var dz = document.getElementById('dz');
  var chips = document.getElementById('chips');
  var submit = document.getElementById('submit');
  var hint = document.getElementById('hint');
  if (!input) return;
  function fmt(b){
    if (b < 1024) return b + ' B';
    var u = ['KB','MB','GB','TB','PB'], i = -1;
    do { b /= 1024; i++; } while (b >= 1024 && i < u.length - 1);
    return b.toFixed(1) + ' ' + u[i];
  }
  function render(){
    var files = input.files;
    chips.innerHTML = '';
    for (var i = 0; i < files.length; i++) {
      var chip = document.createElement('span'); chip.className = 'chip';
      var nm = document.createElement('span'); nm.className = 'nm'; nm.textContent = files[i].name;
      var sz = document.createElement('span'); sz.className = 'sz'; sz.textContent = fmt(files[i].size);
      chip.appendChild(nm); chip.appendChild(sz); chips.appendChild(chip);
    }
    var n = files.length;
    submit.disabled = (n === 0);
    hint.textContent = n ? ('已选择 ' + n + ' 个文件') : '尚未选择文件';
  }
  input.addEventListener('change', render);
  ['dragenter','dragover'].forEach(function(ev){
    dz.addEventListener(ev, function(e){ e.preventDefault(); dz.classList.add('over'); });
  });
  ['dragleave','dragend'].forEach(function(ev){
    dz.addEventListener(ev, function(){ dz.classList.remove('over'); });
  });
  dz.addEventListener('drop', function(e){
    e.preventDefault(); dz.classList.remove('over');
    if (e.dataTransfer && e.dataTransfer.files.length) { input.files = e.dataTransfer.files; render(); }
  });
})();
</script>
{{end}}
</body>
</html>`

const resultHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<title>Canyou · 上传结果</title>
<style>
:root{
  --bg:#f6f7fb; --card:#ffffff; --text:#1f2430; --muted:#8a90a2;
  --border:#e8eaf0; --accent:#4f6ef7; --accent-ink:#ffffff; --accent-soft:#eef1fe;
  --ok:#12a150; --bad:#e5484d; --shadow:0 1px 2px rgba(20,25,40,.04), 0 10px 28px rgba(20,25,40,.06);
}
@media (prefers-color-scheme: dark){
  :root{
    --bg:#0e1116; --card:#161b23; --text:#e6e9f0; --muted:#8b93a7;
    --border:#232a35; --accent:#6d8bff; --accent-ink:#0b1020; --accent-soft:#1b2340;
    --ok:#3ddc84; --bad:#ff6b6b; --shadow:0 1px 2px rgba(0,0,0,.3), 0 12px 32px rgba(0,0,0,.35);
  }
}
*{box-sizing:border-box}
body{margin:0;background:var(--bg);color:var(--text);line-height:1.5;-webkit-font-smoothing:antialiased;
  font-family:-apple-system,BlinkMacSystemFont,"Segoe UI","Microsoft YaHei UI",Roboto,Helvetica,Arial,sans-serif}
a{color:var(--accent);text-decoration:none}
.wrap{max-width:560px;margin:0 auto;padding:40px 16px}
.card{background:var(--card);border:1px solid var(--border);border-radius:18px;box-shadow:var(--shadow);padding:28px}
.badge{width:56px;height:56px;border-radius:50%;display:grid;place-items:center;font-size:26px;background:var(--accent-soft);margin-bottom:16px}
h1{font-size:19px;margin:0 0 4px;font-weight:650}
p{margin:14px 0 6px;font-size:13.5px;font-weight:600}
.ok{color:var(--ok)}
.bad{color:var(--bad)}
ul{list-style:none;margin:8px 0;padding:0}
li{display:flex;align-items:center;gap:9px;padding:11px 14px;border:1px solid var(--border);border-radius:12px;margin-bottom:8px;
  font-size:13.5px;background:var(--bg);word-break:break-all}
.btn{display:inline-block;margin-top:18px;background:var(--accent);color:var(--accent-ink);padding:11px 22px;border-radius:12px;font-weight:650;font-size:14px}
</style>
</head>
<body>
<div class="wrap">
  <div class="card">
    <div class="badge">📤</div>
    <h1>{{.Message}}</h1>
    {{if .Saved}}<p class="ok">成功上传 {{len .Saved}} 个文件</p><ul>{{range .Saved}}<li>📄 {{.}}</li>{{end}}</ul>{{end}}
    {{if .Failed}}<p class="bad">失败 {{len .Failed}} 个</p><ul>{{range .Failed}}<li>⚠️ {{.}}</li>{{end}}</ul>{{end}}
    <a class="btn" href="{{.BackLink}}">返回文件列表</a>
  </div>
</div>
</body>
</html>`
