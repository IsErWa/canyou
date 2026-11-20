package webui

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"strings"
	texttemplate "text/template"
)

// FilesData 只描述页面壳的功能开关和客户端信息；目录内容仍由 API 返回。
type FilesData struct {
	CanUpload bool
	CanChat   bool
	ClientIP  string
	Version   string
}

// 白名单只包含受版本控制的嵌入源码，禁止把请求数据转成可信 JS/CSS。
var styles = loadResources("styles", []string{"theme.css", "base.css", "panels.css", "workspace.css", "gate.css"})
var scripts = loadResources("scripts", []string{"theme-bootstrap.js", "theme.js", "identity.js", "workspace.js", "files.js", "upload-dialog.js", "upload.js", "chat.js", "gate.js"})

func loadResources(dir string, names []string) map[string]string {
	values := make(map[string]string, len(names))
	for _, name := range names {
		data, err := resources.ReadFile(dir + "/" + name)
		if err != nil {
			panic(err)
		}
		values[name] = string(data)
	}
	return values
}

func style(name string) (template.CSS, error) {
	value, ok := styles[name]
	if !ok {
		return "", fmt.Errorf("未许可的样式资源：%s", name)
	}
	return template.CSS(value), nil
}

func script(name string) (template.JS, error) {
	value, ok := scripts[name]
	if !ok {
		return "", fmt.Errorf("未许可的脚本资源：%s", name)
	}
	return template.JS(value), nil
}

var filesTemplate = template.Must(template.New("files.html").Funcs(template.FuncMap{"style": style, "script": script}).ParseFS(resources, "templates/files.html", "templates/partials/*.html"))
var gateTemplate = texttemplate.Must(texttemplate.New("gate.html").Funcs(texttemplate.FuncMap{"style": style, "script": script}).ParseFS(resources, "templates/gate.html", "templates/partials/icons.html"))

// RenderFiles 保留 html/template 对客户端信息的上下文转义。
func RenderFiles(writer io.Writer, data FilesData) error {
	data.Version = displayVersion(data.Version)
	return filesTemplate.ExecuteTemplate(writer, "files.html", data)
}

// RenderGate 保留 text/template 语义；动态初始化数据只允许安全站内路径的 JSON。
func RenderGate(writer io.Writer, target, version string) error {
	data := struct{ Next, Version string }{
		Next: jsonString(safeInternalTarget(target)),
		// text/template 不自动转义；版本只作为 HTML 文本输出。
		Version: template.HTMLEscapeString(displayVersion(version)),
	}
	return gateTemplate.ExecuteTemplate(writer, "gatePage", data)
}

func displayVersion(version string) string {
	if version == "" {
		return "dev"
	}
	return version
}

// safeInternalTarget 只允许跳回本站内部路径，杜绝 //evil.com 之类的开放重定向
func safeInternalTarget(target string) string {
	if !strings.HasPrefix(target, "/") || strings.HasPrefix(target, "//") {
		return "/"
	}
	if strings.ContainsAny(target, "\\\"'<>` ") || strings.Contains(target, "\n") || strings.Contains(target, "\r") {
		return "/"
	}
	return target
}

// jsonString 把值编码成 JS 字面量并转义 < ，防止路径内容截断 <script>
func jsonString(value string) string {
	data, _ := json.Marshal(value)
	return strings.ReplaceAll(string(data), "<", "\\u003c")
}
