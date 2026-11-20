package webui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"
)

// assertInlineScripts 解析实际输出的脚本，Go 编译器不会校验模板中的 JavaScript。
func assertInlineScripts(t *testing.T, html string) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("JavaScript 语法检查需要 Node.js")
	}
	blocks := regexp.MustCompile(`(?s)<script>(.*?)</script>`).FindAllStringSubmatch(html, -1)
	if len(blocks) == 0 {
		t.Fatal("页面没有输出脚本")
	}
	scripts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		scripts = append(scripts, block[1])
	}
	data, err := json.Marshal(scripts)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, node, "-")
	command.Stdin = strings.NewReader("const vm=require('node:vm');const scripts=" + string(data) + ";scripts.forEach((s,i)=>new vm.Script(s,{filename:'inline-'+i+'.js'}));")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("内联 JavaScript 检查失败: %v（超时状态：%v）\n%s", err, ctx.Err(), output)
	}
}

func assertUniquePageIDs(t *testing.T, html string) {
	t.Helper()
	seen := map[string]bool{}
	for _, match := range regexp.MustCompile(`\bid="([^"]+)"`).FindAllStringSubmatch(html, -1) {
		if seen[match[1]] {
			t.Fatalf("重复 DOM ID: %s", match[1])
		}
		seen[match[1]] = true
	}
}

func TestSafeInternalTarget(t *testing.T) {
	good := "/a/b?q=%E4%B8%AD"
	if got := safeInternalTarget(good); got != good {
		t.Fatalf("内部路径被误改: %s", got)
	}
	for _, bad := range []string{"", "//evil.com", "http://evil.com", `/a\<b>`, "/a b", "/a\nb"} {
		if got := safeInternalTarget(bad); got != "/" {
			t.Fatalf("危险目标应回落首页: %q -> %q", bad, got)
		}
	}
}

func TestResponsiveWorkspaceTemplates(t *testing.T) {
	for _, chat := range []bool{false, true} {
		for _, upload := range []bool{false, true} {
			t.Run(fmt.Sprintf("chat=%v/upload=%v", chat, upload), func(t *testing.T) {
				var body bytes.Buffer
				if err := RenderFiles(&body, FilesData{CanChat: chat, CanUpload: upload, ClientIP: "127.0.0.1"}); err != nil {
					t.Fatal(err)
				}
				html := body.String()
				assertUniquePageIDs(t, html)
				for _, marker := range []string{`class="workspace"`, `id="filesPanel"`, `id="pathTitle"`, `id="parentBtn"`, `window.canyouLayout=`, `desktopView`, `compactView`, `navigationVersion:2`, `100dvh`, `window.canyouTheme=`, `window.canyouDeviceReady=`, `id="themeColor"`} {
					if !strings.Contains(html, marker) {
						t.Fatalf("缺少工作台标记: %s", marker)
					}
				}
				for marker, want := range map[string]bool{`id="chatPanel"`: chat, `id="devicesPanel"`: chat, `id="settingsPanel"`: chat, `id="chatTab"`: chat, `id="filesTab"`: chat, `id="uploadDialog"`: upload, `id="uploadToggle"`: upload, `id="themeOptions"`: chat, `id="themeDark"`: chat, `id="themeLight"`: chat, `id="themeSystem"`: chat} {
					if strings.Contains(html, marker) != want {
						t.Fatalf("功能条件渲染错误: %s", marker)
					}
				}
				if strings.Index(html, "window.canyouTheme=") > strings.Index(html, "</head>") || strings.Count(html, "theme.request('/gate/check'") != 1 {
					t.Fatal("主题首屏初始化过晚或设备信息请求不唯一")
				}
				header := strings.SplitN(html, "</header>", 2)[0]
				for _, id := range []string{"chatToggle", "chatDevicesToggle", "chatSettingsToggle", "chatDevicesLabel"} {
					if strings.Contains(header, `id="`+id+`"`) != chat {
						t.Fatalf("顶栏聊天入口条件渲染错误: %s", id)
					}
				}
				if chat {
					button := regexp.MustCompile(`<button\b[^>]*id="chatDevicesToggle"[^>]*>`).FindString(html)
					if button == "" || strings.Contains(button, " hidden") || !strings.Contains(button, " disabled") || !strings.Contains(html, "devicesToggle.disabled=!ready;") || strings.Contains(html, "devicesToggle.hidden=") {
						t.Fatal("在线设备入口应首屏占位，连接就绪前仅禁用")
					}
				}
				if strings.Contains(html, "ZgotmplZ") || strings.Contains(html, "canyou:panel") || strings.Contains(html, `id="chatBack"`) || strings.Contains(html, "function showPage(") {
					t.Fatal("模板转义错误或保留了旧的互斥浮窗事件")
				}
				assertInlineScripts(t, html)
			})
		}
	}
}

func TestGateResponsiveTemplate(t *testing.T) {
	var body bytes.Buffer
	if err := RenderGate(&body, "/", "dev"); err != nil {
		t.Fatal(err)
	}
	html := body.String()
	assertUniquePageIDs(t, html)
	for _, marker := range []string{
		`<main class="box">`, `for="gatePass"`, `role="alert"`, `id="i-lock"`, `100dvh`, `id="themeColor"`, `window.canyouTheme=`, `window.canyouTheme.accept(data.theme)`,
		`id="gateNote" role="status" aria-live="polite" aria-atomic="true"`, `class="gate-spinner" aria-hidden="true"`,
		`@keyframes gate-spin`, `animation:gate-spin .8s linear infinite`,
		`@media(prefers-reduced-motion:reduce){*{animation:none!important;transition:none!important}}`, `note.hidden=true; form.hidden=false;`,
	} {
		if !strings.Contains(html, marker) {
			t.Fatalf("门禁页缺少标记: %s", marker)
		}
	}
	assertInlineScripts(t, html)
}
