package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// assertInlineScripts 解析实际输出的脚本，Go 编译器不会校验模板中的 JavaScript。
func assertInlineScripts(t *testing.T, html string) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("JavaScript 语法检查需要 Node.js")
	}
	blocks := regexp.MustCompile(`(?s)<script\b[^>]*>(.*?)</script>`).FindAllStringSubmatch(html, -1)
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, node, "-")
	command.Stdin = strings.NewReader("const vm=require('node:vm');const scripts=" + string(data) + ";scripts.forEach((s,i)=>new vm.Script(s,{filename:'inline-'+i+'.js'}));")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("内联 JavaScript 语法错误: %v\n%s", err, output)
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

func TestResponsiveWorkspaceTemplates(t *testing.T) {
	for _, chat := range []bool{false, true} {
		for _, upload := range []bool{false, true} {
			t.Run(fmt.Sprintf("chat=%v/upload=%v", chat, upload), func(t *testing.T) {
				var body bytes.Buffer
				if err := listingTemplate.Execute(&body, listingData{CanChat: chat, CanUpload: upload, ClientIP: "127.0.0.1"}); err != nil {
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

// TestResponsiveBrowser 仅显式开启时连接隔离的 Edge DevTools 与门禁预览服务。
// 浏览器启动参数为 --headless=new --remote-debugging-port=19222，预览使用 18082。
func TestResponsiveBrowser(t *testing.T) {
	if os.Getenv("CANYOU_UI_BROWSER") != "1" {
		t.Skip("仅用于响应式浏览器验收")
	}
	request, err := http.NewRequest(http.MethodPut, "http://127.0.0.1:19222/json/new?about:blank", nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	var target struct {
		ID     string `json:"id"`
		Socket string `json:"webSocketDebuggerUrl"`
	}
	err = json.NewDecoder(response.Body).Decode(&target)
	response.Body.Close()
	if err != nil || target.Socket == "" {
		t.Fatalf("无法创建隔离浏览器页面: %v", err)
	}
	t.Cleanup(func() {
		if r, e := client.Get("http://127.0.0.1:19222/json/close/" + target.ID); e == nil {
			r.Body.Close()
		}
	})
	connection, _, err := websocket.DefaultDialer.Dial(target.Socket, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	sequence := 0
	call := func(method string, params any) json.RawMessage {
		t.Helper()
		sequence++
		connection.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if err := connection.WriteJSON(map[string]any{"id": sequence, "method": method, "params": params}); err != nil {
			t.Fatal(err)
		}
		connection.SetReadDeadline(time.Now().Add(20 * time.Second))
		for {
			var reply struct {
				ID     int             `json:"id"`
				Result json.RawMessage `json:"result"`
				Error  json.RawMessage `json:"error"`
			}
			if err := connection.ReadJSON(&reply); err != nil {
				t.Fatal(err)
			}
			if reply.ID != sequence {
				continue
			}
			if len(reply.Error) > 0 {
				t.Fatalf("%s: %s", method, reply.Error)
			}
			return reply.Result
		}
	}
	evaluate := func(script string) json.RawMessage {
		t.Helper()
		result := call("Runtime.evaluate", map[string]any{"expression": script, "awaitPromise": true, "returnByValue": true, "userGesture": true})
		var reply struct {
			Result struct {
				Value json.RawMessage `json:"value"`
			} `json:"result"`
			Exception json.RawMessage `json:"exceptionDetails"`
		}
		if err := json.Unmarshal(result, &reply); err != nil {
			t.Fatal(err)
		}
		if len(reply.Exception) > 0 {
			t.Fatalf("浏览器执行失败: %s", reply.Exception)
		}
		return reply.Result.Value
	}
	resize := func(width, height int) {
		call("Emulation.setDeviceMetricsOverride", map[string]any{"width": width, "height": height, "deviceScaleFactor": 1, "mobile": false})
		evaluate(`new Promise(resolve=>setTimeout(resolve,120))`)
	}
	shot := func(name string) {
		t.Helper()
		evaluate(`Promise.all(document.getAnimations().filter(a=>a.effect&&Number.isFinite(a.effect.getComputedTiming().endTime)).map(a=>a.finished.catch(()=>{})))`)
		result := call("Page.captureScreenshot", map[string]any{"format": "png", "captureBeyondViewport": true})
		var image struct {
			Data string `json:"data"`
		}
		if err := json.Unmarshal(result, &image); err != nil {
			t.Fatal(err)
		}
		data, err := base64.StdEncoding.DecodeString(image.Data)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll("dist", 0o755); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join("dist", name+".png")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		t.Log("验收截图:", path)
	}
	call("Page.enable", map[string]any{})
	call("Page.addScriptToEvaluateOnNewDocument", map[string]any{"source": `
if(location.origin==='http://127.0.0.1:18082'&&!sessionStorage.getItem('__uiTestStarted')){localStorage.removeItem('canyou.ui.state');localStorage.setItem('canyou.device.id',Array.from(crypto.getRandomValues(new Uint8Array(13)),b=>b.toString(16).padStart(2,'0')).join(''));sessionStorage.setItem('__uiTestStarted','1');}
window.__sockets=[];window.WebSocket=class extends WebSocket{constructor(...args){super(...args);window.__sockets.push(this);}};
window.__uiErrors=[];window.addEventListener('error',e=>__uiErrors.push(e.message));window.addEventListener('unhandledrejection',e=>__uiErrors.push(String(e.reason)));
window.__wait=async predicate=>{for(let i=0;i<60;i++){if(predicate())return;await new Promise(r=>setTimeout(r,100));}throw Error('等待界面状态超时');};
window.__assert=(ok,message)=>{if(!ok)throw Error(message);};
window.__themePosts=0;window.__deviceReads=0;
window.__assertView=function(view){
  const el=id=>document.getElementById(id),ids={chat:'chatPanel',devices:'devicesPanel',settings:'settingsPanel'},buttons={chat:'chatToggle',devices:'chatDevicesToggle',settings:'chatSettingsToggle'};
  __assert(canyouLayout.currentView()===view,'当前视图错误：'+canyouLayout.currentView()+'，期望 '+view);
  __assert(!el('chatBack'),'仍保留返回聊天按钮');
  for(const key of Object.keys(ids)){
    const panel=el(ids[key]),button=el(buttons[key]);
    __assert(panel.parentElement.matches('.workspace')&&panel.hidden===(key!==view),'面板不是同级或可见性错误：'+key);
    __assert(button.getAttribute('aria-controls')===ids[key]&&button.getAttribute('aria-expanded')===String(key===view),'顶栏选中或控制关系错误：'+key);
    if(key===view){const r=panel.querySelector('[data-panel-close]').getBoundingClientRect();__assert(r.width>=44&&r.height>=44&&r.right<=innerWidth,'关闭按钮触控区域不足或不可见：'+JSON.stringify(r));__assert(panel.scrollWidth<=panel.clientWidth&&document.documentElement.scrollWidth<=innerWidth,'功能面板横向溢出');}
  }
  __assert(el('chatTab').getAttribute('aria-pressed')===String(view==='chat'),'消息导航误选中');
  __assert(el('filesTab').getAttribute('aria-pressed')===String(view==='files'),'文件导航误选中');
  __assert(el('filesPanel').hidden===(innerWidth<1100&&view!=='files'),'文件区可见性错误');
  __assert(!document.querySelector('#devicesPanel #chatMyInfo, #settingsPanel #chatMyInfo'),'设置或设备仍包含消息身份行');
};
const originalFetch=window.fetch.bind(window);
window.fetch=function(url,options){
  if(url==='/gate/theme')window.__themePosts++;
  if(url==='/gate/check'){
    window.__deviceReads++;
    if(document.getElementById('gateNote')&&!sessionStorage.getItem('__gateLoadingTested')){sessionStorage.setItem('__gateLoadingTested','1');return new Promise((resolve,reject)=>window.__releaseGateCheck=()=>originalFetch(url,options).then(resolve,reject));}
    if(sessionStorage.getItem('__holdDeviceRead')){sessionStorage.removeItem('__holdDeviceRead');return new Promise((resolve,reject)=>window.__releaseDeviceRead=()=>originalFetch(url,options).then(resolve,reject));}
    if(sessionStorage.getItem('__failDeviceRead')){sessionStorage.removeItem('__failDeviceRead');return Promise.reject(new Error('模拟设备信息网络失败'));}
    if(sessionStorage.getItem('__denyDeviceRead')){sessionStorage.removeItem('__denyDeviceRead');return Promise.resolve(new Response(JSON.stringify({ok:false,allowed:false}),{status:200}));}
  }
  return originalFetch(url,options);
};
if(sessionStorage.getItem('__noUICache')){
  sessionStorage.removeItem('__noUICache');
  for(const name of ['getItem','setItem']){const native=Storage.prototype[name];Storage.prototype[name]=function(key,...args){if(key==='canyou.ui.state')throw new Error('模拟导航缓存不可用');return native.call(this,key,...args);};}
}
if(sessionStorage.getItem('__noThemeCache')){
  sessionStorage.removeItem('__noThemeCache');
  for(const name of ['getItem','setItem']){const native=Storage.prototype[name];Storage.prototype[name]=function(key,...args){if(String(key).startsWith('canyou.theme.'))throw new Error('模拟主题缓存不可用');return native.call(this,key,...args);};}
}`})
	resize(1440, 900)
	call("Page.navigate", map[string]any{"url": "http://127.0.0.1:18082/"})
	call("Emulation.setEmulatedMedia", map[string]any{"features": []map[string]string{{"name": "prefers-color-scheme", "value": "light"}}})
	gatePage := evaluate(`(async()=>{for(let i=0;i<60&&!document.getElementById('gatePass')&&!document.getElementById('fileList');i++)await new Promise(r=>setTimeout(r,100));return !!document.getElementById('gatePass');})()`)
	if string(gatePage) == "true" {
		evaluate(`__wait(()=>typeof __releaseGateCheck==='function')`)
		for _, size := range [][2]int{{1440, 900}, {390, 844}, {360, 640}} {
			resize(size[0], size[1])
			for _, theme := range []string{"light", "dark"} {
				call("Emulation.setEmulatedMedia", map[string]any{"features": []map[string]string{{"name": "prefers-color-scheme", "value": theme}, {"name": "prefers-reduced-motion", "value": "no-preference"}}})
				evaluate(`(async()=>{
  const note=document.getElementById('gateNote'),spinner=note.querySelector('.gate-spinner'),form=document.getElementById('gateForm');
  __assert(!note.hidden&&form.hidden&&spinner&&spinner.getClientRects().length,'核验等待时加载图标未显示');
  __assert(note.getAttribute('role')==='status'&&spinner.getAttribute('aria-hidden')==='true','加载提示缺少无障碍标记');
  __assert(getComputedStyle(spinner).animationName==='gate-spin'&&getComputedStyle(spinner).animationIterationCount==='infinite','核验加载动画未启动');
  const before=note.getBoundingClientRect().toJSON(),transform=getComputedStyle(spinner).transform;
  await new Promise(resolve=>setTimeout(resolve,160));
  __assert(getComputedStyle(spinner).transform!==transform,'核验加载图标未持续旋转');
  const after=note.getBoundingClientRect();
  for(const key of ['x','y','width','height'])__assert(Math.abs(before[key]-after[key])<1,'核验动画导致布局跳动');
  __assert(document.documentElement.scrollWidth<=innerWidth,'核验加载页面横向溢出');
  return true;
})()`)
				shot(fmt.Sprintf("gate-loading-%s-%dx%d", theme, size[0], size[1]))
			}
		}
		call("Emulation.setEmulatedMedia", map[string]any{"features": []map[string]string{{"name": "prefers-color-scheme", "value": "light"}, {"name": "prefers-reduced-motion", "value": "reduce"}}})
		evaluate(`__assert(getComputedStyle(document.querySelector('.gate-spinner')).animationName==='none','减少动态效果偏好未生效');true`)
		call("Emulation.setEmulatedMedia", map[string]any{"features": []map[string]string{{"name": "prefers-color-scheme", "value": "light"}, {"name": "prefers-reduced-motion", "value": "no-preference"}}})
		evaluate(`__releaseGateCheck();true`)
		resize(390, 844)
		evaluate(`(async()=>{await __wait(()=>!document.getElementById('gateForm').hidden);__assert(document.getElementById('gateNote').hidden&&!document.querySelector('.gate-spinner').getClientRects().length,'核验结束后加载图标未隐藏');__assert(document.documentElement.scrollWidth<=innerWidth,'门禁页面横向溢出');return true;})()`)
		shot("workspace-gate-mobile")
		resize(1440, 900)
		shot("workspace-gate-desktop")
		evaluate(`document.getElementById('gatePass').value='1234';document.getElementById('gateForm').requestSubmit();true`)
	}
	time.Sleep(500 * time.Millisecond)
	evaluate(`(async()=>{for(let i=0;i<60;i++){if(document.getElementById('chatStatus')?.classList.contains('ok')&&document.querySelector('.row'))return true;await new Promise(r=>setTimeout(r,100));}throw Error('主页或聊天室未就绪');})()`)
	evaluate(`__assert(Array.isArray(__sockets)&&__sockets.length===1,'连接监测初始化失败');__assert(!document.getElementById('chatPanel').hidden&&document.activeElement.id!=='chatText','首次桌面视图或焦点错误');true`)
	assertFileAlignment := func() {
		t.Helper()
		evaluate(`(()=>{
  canyouLayout.open('files');
  const rect=e=>e.getBoundingClientRect(),center=r=>(r.top+r.bottom)/2;
  const rows=[...document.querySelectorAll('#fileList .row')];
  __assert(rows.length>30,'缺少文件布局样例');
  for(const row of rows){
    const link=row.querySelector('.name a'),label=row.querySelector('.file-label'),icon=row.querySelector('.fi'),meta=row.querySelector('.file-meta');
    __assert(label&&label.textContent===link.textContent,'文件名结构或内容改变');
    const text=rect(label),pic=rect(icon),hit=rect(link),mobile=innerWidth<768;
    __assert(getComputedStyle(label).fontSize===(mobile?'15px':'14px'),'文件名字号不符合当前断点');
    const graphic=rect(icon.querySelector('.icon')),iconSize=mobile?22:20;
    __assert(graphic.width===iconSize&&graphic.height===iconSize,'文件图标尺寸不符合当前断点');
    const metaStyle=getComputedStyle(meta),metaWidth=meta.clientWidth-parseFloat(metaStyle.paddingLeft||0)-parseFloat(metaStyle.paddingRight||0);
    const infoFont=mobile?Math.max(8,Math.min(10,metaWidth*.06)):12;
    for(const part of meta.children)__assert(Math.abs(parseFloat(getComputedStyle(part).fontSize)-infoFont)<.02,'时间或文件大小未按可用宽度调整字号');
    if(mobile){
      __assert(Math.abs(center(text)-center(pic))<1,'手机图标与文件名未垂直对齐');
      const check=rect(row.querySelector('.pick')),checkHit=rect(row.querySelector('.ck')),download=rect(row.querySelector('.dl'));
      __assert(Math.abs(center(check)-center(text))<1,'手机多选框与文件名未垂直对齐');
      __assert(Math.abs(center(download)-center(text))<1,'手机下载按钮与文件名未垂直对齐');
      for(const area of [hit,checkHit,download])__assert(area.width>=44&&area.height>=44,'手机文件操作点击区域不足');
      __assert(text.height<=parseFloat(getComputedStyle(label).lineHeight)*2+1,'长文件名超过两行');
      const parts=[...meta.children].filter(e=>e.textContent).sort((a,b)=>rect(a).top-rect(b).top||rect(a).left-rect(b).left);
      __assert(parts.length&&parts[0].classList.contains('date'),'手机文件信息应先展示时间');
      const nameBox=rect(row.querySelector('.name')),rowBox=rect(row),middle=center(rowBox),metaBox=rect(meta);
      for(const item of [check,graphic,text,rect(row.querySelector('.dl .icon'))])__assert(Math.abs(center(item)-middle)<1,'手机主行元素未相对整条记录上下居中');
      __assert(Math.abs(pic.left-nameBox.left)<1&&Math.abs(text.left-hit.left)<1&&getComputedStyle(link).textAlign==='left','手机文件图标或文件名未恢复左对齐');
      __assert(Math.abs(rect(parts[0]).left-text.left)<1,'时间未与文件名左侧对齐');
      for(const part of parts){
        const info=rect(part);
        __assert(Math.abs(info.top-metaBox.top)<1,'时间与大小应保持在同一行');
        __assert(info.top>=Math.max(text.bottom,pic.bottom)+1,'时间或大小与文件名、图标重叠');
        __assert(info.left>=text.left-1&&info.right<=nameBox.right+1,'文件信息超出下方可用区域');
        __assert(info.bottom<=rowBox.bottom-1,'文件信息超出行边界');
      }
      if(parts.length===2)__assert(rect(parts[0]).right<rect(parts[1]).left,'时间与大小相互重叠');
      __assert(rowBox.height<=74,'手机文件行高度过大');
    }else{
      __assert(Math.abs(center(pic)-center(hit))<1,'桌面图标与文件名未对齐');
      __assert(getComputedStyle(meta).display==='contents','桌面大小与时间分列被改变');
    }
  }
  __assert(document.getElementById('fileList').scrollWidth<=document.getElementById('fileList').clientWidth,'文件列表横向溢出');
  return true;
})()`)
	}
	for _, size := range [][2]int{{1440, 900}, {1100, 800}, {1024, 768}, {768, 1024}, {390, 844}, {360, 640}, {844, 390}} {
		resize(size[0], size[1])
		// 平级入口、直接关闭与窄屏导航选中态，不依赖消息父页。
		evaluate(`(()=>{
const el=id=>document.getElementById(id),devices=el('chatDevicesToggle'),settings=el('chatSettingsToggle');
canyouLayout.open('files');__assertView('files');
const actions=[el('uploadToggle'),el('chatToggle'),devices,settings].filter(b=>b.getClientRects().length);
let right=document.querySelector('.tb-titles').getBoundingClientRect().right;
for(const b of actions){const r=b.getBoundingClientRect();__assert(b.closest('.tb-actions')&&r.left>=right&&r.right<=innerWidth&&Math.abs(r.top-actions[0].getBoundingClientRect().top)<1,'顶栏入口溢出、重叠或不同排');if(innerWidth<768)__assert(r.width>=44&&r.height>=44,'手机入口触控范围不足');right=r.right;}
__assert(devices.querySelector('svg use')&&el('chatDevicesLabel').textContent==='在线设备('+el('chatDevicesCount').textContent+')','设备计数更新破坏图标或标签');
for(const origin of ['files','chat'])for(const view of ['devices','settings']){
  canyouLayout.open(origin);const entry=view==='devices'?devices:settings;
  entry.focus();entry.click();__assertView(view);
  el(view+'Close').click();__assertView('files');__assert(document.activeElement===entry,'关闭未恢复入口焦点');
  entry.click();__assertView(view);entry.click();__assertView('files');
}
settings.click();devices.click();__assertView('devices');settings.click();__assertView('settings');
(innerWidth>=1100?el('chatToggle'):el('chatTab')).click();__assertView('chat');
if(innerWidth>=1100){el('chatToggle').click();__assertView('files');}else{el('chatTab').click();__assertView('chat');el('filesTab').click();__assertView('files');}
canyouLayout.open('chat');el('chatClose').click();__assertView('files');__assert(document.activeElement.id===(innerWidth>=1100?'chatToggle':'chatTab'),'关闭消息未恢复入口焦点');
__assert(__sockets.length===1,'顶栏切页新建了连接');return true;
})()`)
		for _, view := range []string{"chat", "devices", "settings"} {
			evaluate(fmt.Sprintf(`canyouLayout.open(%q);__assertView(%q);document.activeElement.blur();true`, view, view))
			shot(fmt.Sprintf("panels-%s-light-%dx%d", view, size[0], size[1]))
			call("Input.dispatchKeyEvent", map[string]any{"type": "keyDown", "key": "Escape", "code": "Escape", "windowsVirtualKeyCode": 27})
			call("Input.dispatchKeyEvent", map[string]any{"type": "keyUp", "key": "Escape", "code": "Escape", "windowsVirtualKeyCode": 27})
			evaluate(`__assertView('files');true`)
		}
		value := evaluate(`(()=>{const d=document,w=innerWidth,h=innerHeight,rect=id=>d.getElementById(id).getBoundingClientRect();canyouLayout.open('files');__assert(!d.getElementById('filesPanel').hidden,'文件区不可见');__assert(d.documentElement.scrollWidth<=w,'文件页面横向溢出');__assert(d.getElementById('fileList').scrollWidth<=d.getElementById('fileList').clientWidth,'文件列表横向溢出');canyouLayout.open('chat');const panel=rect('chatPanel'),send=rect('chatSend');__assert(send.bottom<=panel.bottom&&send.top>=panel.top,'发送按钮被裁切');__assert(d.documentElement.scrollWidth<=w,'聊天页面横向溢出');if(w>=1100)__assert(Math.round(panel.width)===360&&!d.getElementById('filesPanel').hidden,'桌面双栏错误');else __assert(d.getElementById('filesPanel').hidden,'窄屏未切换单栏');d.getElementById('uploadToggle').click();const up=rect('uploadDialog');__assert(up.left>=0&&up.right<=w&&up.bottom<=h,'上传弹窗超出视口');d.getElementById('uploadDialog').close();return {width:w,height:h,chatWidth:Math.round(panel.width),sendBottom:Math.round(send.bottom)};})()`)
		t.Logf("尺寸验收 %dx%d: %s", size[0], size[1], value)
		assertFileAlignment()
		if size[0] < 768 {
			shot(fmt.Sprintf("files-aligned-light-%dx%d", size[0], size[1]))
			evaluate(`(()=>{const long=[...document.querySelectorAll('.file-label')].find(e=>e.textContent.length>50);long.scrollIntoView({block:'center'});return true;})()`)
			shot(fmt.Sprintf("files-aligned-long-%dx%d", size[0], size[1]))
			evaluate(`document.getElementById('fileList').scrollTop=0;true`)
		}
		evaluate(`canyouLayout.open('chat');new Promise(resolve=>setTimeout(resolve,200))`)
		shot(fmt.Sprintf("workspace-light-%dx%d", size[0], size[1]))
	}
	resize(320, 640)
	assertFileAlignment()
	evaluate(`(()=>{const size=[...document.querySelectorAll('.file-meta .size')].find(e=>e.textContent);window.__sizeProbe={element:size,value:size.textContent};size.textContent='1023.9 GB';__assert(parseFloat(getComputedStyle(size).fontSize)<10,'窄屏元信息字号未缩小');return true;})()`)
	assertFileAlignment()
	evaluate(`document.getElementById('fileList').scrollTop=0;true`)
	shot("files-vertical-320x640")
	evaluate(`__sizeProbe.element.textContent=__sizeProbe.value;delete window.__sizeProbe;true`)
	resize(1440, 900)
	evaluate(`canyouLayout.open('chat');true`)
	shot("workspace-desktop")
	// 窄屏切换保留目录选择、文件滚动与聊天草稿。
	resize(390, 844)
	evaluate(`(()=>{canyouLayout.open('files');const box=document.querySelector('.pick');box.click();document.getElementById('fileList').scrollTop=720;window.__fileScroll=document.getElementById('fileList').scrollTop;document.getElementById('chatTab').click();document.getElementById('chatText').value='保留的聊天草稿';document.getElementById('filesTab').click();__assert(box.checked,'切换丢失勾选');__assert(document.getElementById('fileList').scrollTop===__fileScroll,'切换丢失文件滚动');__assert(document.getElementById('chatText').value==='保留的聊天草稿','切换丢失草稿');return true;})()`)
	resize(1440, 900)
	resize(390, 844)
	evaluate(`(()=>{__assert(document.querySelector('.pick').checked,'跨断点丢失勾选');__assert(document.getElementById('fileList').scrollTop===__fileScroll,'跨断点丢失滚动');document.getElementById('selNone').click();document.getElementById('selAll').click();__assert([...document.querySelectorAll('.pick')].every(b=>b.checked),'全选失败');document.getElementById('selNone').click();document.getElementById('fileList').scrollTop=0;return true;})()`)
	shot("workspace-mobile-files")
	// 目录导航和浏览器历史继续使用固定地址。
	evaluate(`(async()=>{document.querySelector('[data-directory]').click();await __wait(()=>document.querySelector('[aria-current="page"]')?.textContent==='子目录');__assert(location.pathname==='/','导航改变地址');history.back();await __wait(()=>document.querySelectorAll('.pick').length>30);history.forward();await __wait(()=>document.querySelector('[aria-current="page"]')?.textContent==='子目录');document.getElementById('parentBtn').click();await __wait(()=>document.querySelectorAll('.pick').length>30);return true;})()`)
	// 深路径、空目录和失败重试仅刷新文件区，不重置聊天。
	evaluate(`(async()=>{
  for(const name of ['子目录','项目资料','交付文件','最终版本']){
    [...document.querySelectorAll('[data-directory]')].find(e=>e.textContent===name).click();
    await __wait(()=>document.querySelector('[aria-current="page"]')?.textContent===name&&document.getElementById('fileList').getAttribute('aria-busy')==='false');
  }
  __assert(document.getElementById('fileList').textContent.includes('还是空的'),'空目录反馈缺失');
  __assert(document.documentElement.scrollWidth<=innerWidth,'深路径撑宽页面');
  __assert(document.getElementById('chatText').value==='保留的聊天草稿','目录切换重置聊天');
  document.getElementById('pathTitle').querySelector('button').click();await __wait(()=>document.querySelectorAll('.pick').length>30);
  const original=window.fetch;window.fetch=(url,options)=>url==='/api/list'?Promise.reject(Error('模拟断网')):original(url,options);
  document.querySelector('[data-directory]').click();await __wait(()=>document.getElementById('fileList').textContent.includes('网络错误'));
  window.fetch=original;[...document.querySelectorAll('#fileList button')].find(e=>e.textContent==='重新加载').click();
  await __wait(()=>document.querySelector('[aria-current="page"]')?.textContent==='子目录');
  document.getElementById('parentBtn').click();await __wait(()=>document.querySelectorAll('.pick').length>30);return true;
})()`)
	// 原生预览、快速重开时的关闭事件竞态以及媒体释放。
	evaluate(`(async()=>{
  document.querySelector('a.preview[data-path$=".png"]').click();const lb=document.querySelector('.lightbox');
  __assert(lb.open&&lb.querySelector('img'),'图片未预览');
  if(!lb.querySelector('.lb-next').hidden)lb.querySelector('.lb-next').click();
  lb.querySelector('.lb-x').click();__assert(!lb.open&&!lb.querySelector('img'),'关闭没有释放图片');
  document.querySelector('a.preview[data-path$=".wav"]').click();await new Promise(r=>setTimeout(r,80));
  __assert(lb.open&&!lb.hidden&&lb.querySelector('audio')?.controls,'快速重开被旧关闭事件清空');
  await __wait(()=>lb.querySelector('audio').readyState>0);
  const audio=lb.querySelector('audio');await audio.play().catch(e=>{throw Error('WAV 播放失败：'+e.message);});__assert(!audio.paused,'音频没有开始播放');
  lb.querySelector('.lb-x').click();__assert(!lb.querySelector('audio')&&audio.paused&&!audio.hasAttribute('src'),'关闭没有停止并释放音频');
  const canvas=document.createElement('canvas');canvas.width=160;canvas.height=100;
  const stream=canvas.captureStream(10),recorder=new MediaRecorder(stream,{mimeType:'video/webm;codecs=vp8'}),chunks=[];
  recorder.ondataavailable=e=>chunks.push(e.data);const stopped=new Promise(r=>recorder.onstop=r);
  recorder.start();const ctx=canvas.getContext('2d');
  for(let i=0;i<6;i++){ctx.fillStyle=i%2?'#4263eb':'#22a06b';ctx.fillRect(0,0,160,100);await new Promise(r=>setTimeout(r,100));}
  recorder.stop();await stopped;stream.getTracks().forEach(t=>t.stop());
  const url=URL.createObjectURL(new Blob(chunks,{type:'video/webm'}));
  document.querySelector('a.preview[data-path$=".mp4"]').click();const video=lb.querySelector('video');
  video.src=url;video.loop=true;video.load();await video.play().catch(e=>{throw Error('WebM 播放失败（'+chunks.map(b=>b.size)+' 字节）：'+e.message);});__assert(!video.paused&&video.videoWidth===160,'生成的视频未播放');
  const nav=[...lb.querySelectorAll('.lb-nav')].find(b=>!b.hidden);nav.click();
  __assert(!video.isConnected&&video.paused&&!video.hasAttribute('src'),'切换没有停止并释放视频');
  lb.querySelector('.lb-x').click();URL.revokeObjectURL(url);return true;
})()`)
	downloadDir := t.TempDir()
	call("Browser.setDownloadBehavior", map[string]any{"behavior": "allow", "downloadPath": downloadDir})
	evaluate(`document.querySelector('.dl[download="示例文件.txt"]').click();true`)
	deadline := time.Now().Add(5 * time.Second)
	for {
		data, err := os.ReadFile(filepath.Join(downloadDir, "示例文件.txt"))
		if err == nil {
			if string(data) != "门禁验收样例" {
				t.Fatalf("下载内容错误: %q", data)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("单文件下载未完成")
		}
		time.Sleep(50 * time.Millisecond)
	}
	evaluate(`(()=>{document.getElementById('selNone').click();const boxes=[...document.querySelectorAll('.pick')].filter(e=>e.value.endsWith('.txt')).slice(0,2);boxes.forEach(e=>e.click());document.getElementById('zipBtn').click();return true;})()`)
	deadline = time.Now().Add(5 * time.Second)
	for {
		paths, _ := filepath.Glob(filepath.Join(downloadDir, "*.zip"))
		if len(paths) > 0 {
			archive, err := zip.OpenReader(paths[0])
			if err == nil {
				n := len(archive.File)
				archive.Close()
				if n != 2 {
					t.Fatalf("打包条数错误: %d", n)
				}
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("批量下载未完成")
		}
		time.Sleep(50 * time.Millisecond)
	}
	evaluate(`document.getElementById('selNone').click();true`)
	folderDir := t.TempDir()
	call("Browser.setDownloadBehavior", map[string]any{"behavior": "allow", "downloadPath": folderDir})
	evaluate(`document.querySelector('.dlpick').click();true`)
	deadline = time.Now().Add(5 * time.Second)
	for {
		paths, _ := filepath.Glob(filepath.Join(folderDir, "*.zip"))
		if len(paths) > 0 {
			archive, err := zip.OpenReader(paths[0])
			if err == nil {
				found := false
				for _, entry := range archive.File {
					if strings.HasSuffix(entry.Name, "内层文件.txt") {
						found = true
					}
				}
				archive.Close()
				if !found {
					t.Fatal("文件夹打包缺少子文件")
				}
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("文件夹下载未完成")
		}
		time.Sleep(50 * time.Millisecond)
	}
	// 上传队列、上传中锁定、成功与错误反馈；请求仅发送至临时上传目录。
	evaluate(`(async()=>{document.getElementById('uploadToggle').click();const input=document.getElementById('fileInput'),dt=new DataTransfer();for(let i=0;i<7;i++)dt.items.add(new File(['浏览器上传验收'],'浏览器上传-'+i+'.txt',{type:'text/plain'}));input.files=dt.files;input.dispatchEvent(new Event('change'));__assert(!document.getElementById('pager').hidden,'上传分页不存在');document.getElementById('uploadClose').click();document.getElementById('uploadToggle').click();__assert(input.files.length===7,'关闭丢失待上传文件');document.getElementById('uploadPanel').requestSubmit();__assert(input.disabled&&document.getElementById('submit').disabled,'上传时未锁定');await __wait(()=>!input.disabled);__assert(document.getElementById('uploadResult').classList.contains('ok'),'上传未成功');__assert(input.files.length===0,'成功未清空队列');document.getElementById('uploadClose').click();return true;})()`)
	// 在浏览器端注入限定于上传接口的错误响应，验证反馈及失败后保留文件。
	evaluate(`(async()=>{
  document.getElementById('uploadToggle').click();const input=document.getElementById('fileInput'),dt=new DataTransfer();
  dt.items.add(new File(['失败回归'],'失败回归.txt'));input.files=dt.files;input.dispatchEvent(new Event('change'));
  const original=window.fetch;
  try {
    for(const mode of ['partial','failure','network']){
      window.fetch=(url,options)=>url==='/upload' ? (mode==='network' ? Promise.reject(Error('模拟断网')) : Promise.resolve(new Response(JSON.stringify({ok:mode==='partial',message:'模拟上传反馈',failed:['失败回归.txt']}),{headers:{'Content-Type':'application/json'}}))) : original(url,options);
      document.getElementById('uploadPanel').requestSubmit();await __wait(()=>!input.disabled);
      __assert(input.files.length===1,'失败时清空了待上传文件');
      __assert(document.getElementById('uploadResult').classList.contains(mode==='partial'?'partial':'bad'),'错误反馈分类错误');
    }
  } finally {window.fetch=original;document.getElementById('uploadClose').click();}
  return true;
})()`)
	// 拖拽、移除、分页以及在途上传关闭后切页；完成后仍恢复反馈。
	evaluate(`(async()=>{
  document.getElementById('uploadToggle').click();document.querySelector('.chip-x').click();
  const input=document.getElementById('fileInput'),dt=new DataTransfer();
  for(let i=0;i<7;i++)dt.items.add(new File(['拖拽上传'],'拖拽-'+i+'.txt'));
  document.getElementById('dz').dispatchEvent(new DragEvent('drop',{dataTransfer:dt,bubbles:true,cancelable:true}));
  document.querySelector('#pager button').click();__assert(document.querySelectorAll('.chip').length===5,'每页不是五项');
  document.querySelector('.chip-x').click();__assert(input.files.length===6,'移除文件未更新队列');
  const original=window.fetch;let release,requests=0;
  window.fetch=(url,options)=>url==='/upload'?(requests++,new Promise(resolve=>release=()=>resolve(original(url,options)))):original(url,options);
  try {
    document.getElementById('uploadPanel').requestSubmit();document.getElementById('uploadPanel').requestSubmit();
    __assert(requests===1,'重复提交了在途上传');document.getElementById('uploadClose').click();
    document.getElementById('chatTab').click();__assert(document.getElementById('filesPanel').hidden,'上传中不能切页');
    document.getElementById('uploadToggle').click();__assert(input.files.length===6&&input.disabled,'上传队列未保持锁定');
    release();await __wait(()=>!input.disabled);__assert(document.getElementById('uploadResult').classList.contains('ok'),'隐藏上传未完成');
  } finally {window.fetch=original;document.getElementById('uploadClose').click();}
  return true;
})()`)
	// 手机 Enter 放行换行，不触发发送；输入法 Enter 也不发送。
	evaluate(`(()=>{document.getElementById('chatTab').click();const input=document.getElementById('chatText');input.value='手机草稿';input.dispatchEvent(new Event('input'));const e=new KeyboardEvent('keydown',{key:'Enter',bubbles:true,cancelable:true});input.dispatchEvent(e);__assert(!e.defaultPrevented&&input.value==='手机草稿','手机 Enter 被拦截发送');return true;})()`)
	resize(1440, 900)
	evaluate(`(async()=>{const input=document.getElementById('chatText');input.value='响应式工作台验收消息';input.dispatchEvent(new Event('input'));const ime=new KeyboardEvent('keydown',{key:'Enter',isComposing:true,bubbles:true,cancelable:true});input.dispatchEvent(ime);__assert(!ime.defaultPrevented,'中文组合输入被发送');document.getElementById('chatForm').requestSubmit();await __wait(()=>input.value==='');__assert([...document.querySelectorAll('.chat-body')].some(e=>e.textContent==='响应式工作台验收消息'),'消息未显示');document.getElementById('chatSettingsToggle').click();const swatches=[...document.querySelectorAll('.chat-swatch')];swatches.find(e=>e.getAttribute('aria-pressed')!=='true').click();__assert(!document.getElementById('chatSaveColor').disabled,'头像颜色不可保存');document.getElementById('chatSaveColor').click();await __wait(()=>document.getElementById('settingsStatus').textContent==='头像颜色已更新');document.getElementById('chatName').value='响应式验收设备';document.getElementById('chatRename').click();await __wait(()=>document.getElementById('settingsStatus').textContent==='设备名称已保存');document.getElementById(innerWidth>=1100?'chatToggle':'chatTab').click();return true;})()`)
	// 另一隔离身份向公共频道发送，验证隐藏视图的未读与颜色/设备广播。
	evaluate(`(async()=>{
  __assert(__sockets.length===1,'布局切换重复创建 WebSocket');
  const did='b0b0b0b0b0b0b0b0b0b0b0b0b0';
  await fetch('/gate/unlock',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({did,password:'1234',name:'第二个验收设备'})});
  window.__peer=new WebSocket('ws://'+location.host+'/ws/chat?id='+did);window.__peerEvents=[];
  __peer.addEventListener('message',e=>__peerEvents.push(JSON.parse(e.data)));
  await __wait(()=>__peerEvents.some(e=>e.type==='ready'));
  canyouLayout.open('files');__peer.send(JSON.stringify({type:'message',text:'来自第二台设备的验收消息',requestId:'browser-peer'}));
  await __wait(()=>[...document.querySelectorAll('[data-unread]')].every(e=>!e.hidden));
  canyouLayout.open('chat');__assert([...document.querySelectorAll('[data-unread]')].every(e=>e.hidden),'进入聊天未清除未读');
  for(const view of ['devices','settings']){
    canyouLayout.open(view);const before=Number(document.querySelector('[data-unread]').textContent);
    __peer.send(JSON.stringify({type:'message',text:view+'页期间的消息',requestId:'unread-'+view}));
    await __wait(()=>[...document.querySelectorAll('[data-unread]')].every(e=>!e.hidden&&Number(e.textContent)>before));__assertView(view);
    canyouLayout.close();__assertView('files');__assert([...document.querySelectorAll('[data-unread]')].every(e=>!e.hidden),'关闭平级面板误清未读');
  }
  canyouLayout.open('chat');__assert([...document.querySelectorAll('[data-unread]')].every(e=>e.hidden),'切回消息未清未读');
  document.getElementById('chatSettingsToggle').click();
  __peer.send(JSON.stringify({type:'color',color:3,requestId:'browser-color'}));
  await __wait(()=>__peerEvents.some(e=>e.type==='recolored'));
  await __wait(()=>{const avatars=[...document.querySelectorAll('.chat-avatar')].filter(e=>e.dataset.cid===did);return avatars.length>0&&avatars.every(e=>e.style.backgroundColor==='rgb(242, 136, 75)');});
  __peer.send(JSON.stringify({type:'rename',name:'同步后的第二设备',requestId:'browser-rename'}));
  await __wait(()=>[...document.querySelectorAll('.chat-device-name')].some(e=>e.textContent==='同步后的第二设备'));
  document.getElementById(innerWidth>=1100?'chatToggle':'chatTab').click();
  document.getElementById('uploadToggle').focus();document.getElementById('uploadToggle').click();
  __peer.send(JSON.stringify({type:'message',text:'弹窗期间的消息',requestId:'browser-modal'}));
  await __wait(()=>[...document.querySelectorAll('[data-unread]')].every(e=>!e.hidden));
  document.getElementById('uploadClose').click();await __wait(()=>[...document.querySelectorAll('[data-unread]')].every(e=>e.hidden));
  __assert(document.activeElement.id==='uploadToggle','上传关闭没有恢复焦点');
  const text=document.getElementById('chatText');text.value='长消息 '+('连续长文本abcdefgh'.repeat(80));text.dispatchEvent(new Event('input'));
  text.dispatchEvent(new KeyboardEvent('keydown',{key:'Enter',shiftKey:true,bubbles:true,cancelable:true}));
  __assert(text.value.startsWith('长消息'),'Shift+Enter 错误发送');
  text.dispatchEvent(new KeyboardEvent('keydown',{key:'Enter',bubbles:true,cancelable:true}));await __wait(()=>text.value==='');
  __assert(document.getElementById('chatLog').scrollWidth<=document.getElementById('chatLog').clientWidth,'长消息横向溢出');
  document.getElementById('chatSettingsToggle').click();const limit=document.getElementById('chatLimit'),old=Number(limit.value),confirm=window.confirm;
  let prompts=0;window.confirm=()=>{prompts++;return false;};limit.value=old-1;document.getElementById('chatSaveLimit').click();
  __assert(prompts===1&&Number(limit.value)===old,'降低上限没有确认或取消未还原');
  window.confirm=()=>{prompts++;return true;};limit.value=old-1;document.getElementById('chatSaveLimit').click();
  await __wait(()=>__peerEvents.some(e=>e.type==='settings'&&e.limit===old-1));
  __assert(prompts===2,'确认后未提交保留上限');window.confirm=confirm;
  limit.value=old;document.getElementById('chatSaveLimit').click();await __wait(()=>__peerEvents.some(e=>e.type==='settings'&&e.limit===old));
  document.getElementById(innerWidth>=1100?'chatToggle':'chatTab').click();__peer.close(1000);return true;
})()`)
	// 桌面和手机：断线不跳出当前平级面板，重连不强制进入消息。
	for _, size := range [][2]int{{1440, 900}, {390, 844}} {
		resize(size[0], size[1])
		for _, view := range []string{"devices", "settings"} {
			evaluate(fmt.Sprintf(`(async()=>{
  canyouLayout.open(%q);const input=document.getElementById('chatText'),devices=document.getElementById('chatDevicesToggle');input.value='断线时保留的草稿';
  const before=devices.getBoundingClientRect(),count=__sockets.length;
  __sockets.filter(s=>s.readyState===WebSocket.OPEN).at(-1).close();await __wait(()=>devices.disabled);__assertView(%q);
  __assert(!devices.hidden&&Math.abs(devices.getBoundingClientRect().left-before.left)<1,'断线时设备入口消失或顶栏跳动');
  if(canyouLayout.currentView()==='settings'){
    __assert(!document.getElementById('themeOptions').disabled&&document.getElementById('chatRename').disabled&&document.getElementById('chatSaveColor').disabled&&document.getElementById('chatSaveLimit').disabled,'设置断线时控制状态错误');
    document.getElementById('themeLight').click();await __wait(()=>!document.getElementById('themeOptions').disabled);__assert(document.documentElement.dataset.theme==='light','断线无法切换外观');
    document.getElementById('themeSystem').click();await __wait(()=>!document.getElementById('themeOptions').disabled);
  }else{
    document.getElementById('devicesClose').click();__assertView('files');
    __assert(document.activeElement.id===(innerWidth<1100?'filesTab':'filesTitle'),'禁用入口关闭后焦点未回退');
    canyouLayout.open('devices');
  }
  await __wait(()=>__sockets.length>count&&!devices.disabled);__assertView(%q);
  __assert(input.value==='断线时保留的草稿','重连丢失草稿');input.value='';input.dispatchEvent(new Event('input'));return true;
})()`, view, view, view))
		}
	}
	resize(1440, 900)
	evaluate(`canyouLayout.open('settings');true`)
	// 模态键盘焦点约束与 Escape 通过真实输入事件验收。
	evaluate(`document.getElementById('uploadToggle').focus();document.getElementById('uploadToggle').click();true`)
	for i := 0; i < 6; i++ {
		call("Input.dispatchKeyEvent", map[string]any{"type": "keyDown", "key": "Tab", "code": "Tab", "windowsVirtualKeyCode": 9})
		call("Input.dispatchKeyEvent", map[string]any{"type": "keyUp", "key": "Tab", "code": "Tab", "windowsVirtualKeyCode": 9})
		evaluate(`__assert(document.activeElement===document.body||document.getElementById('uploadDialog').contains(document.activeElement),'焦点穿透模态窗口');true`)
	}
	call("Input.dispatchKeyEvent", map[string]any{"type": "keyDown", "key": "Escape", "code": "Escape", "windowsVirtualKeyCode": 27})
	call("Input.dispatchKeyEvent", map[string]any{"type": "keyUp", "key": "Escape", "code": "Escape", "windowsVirtualKeyCode": 27})
	evaluate(`(async()=>{await __wait(()=>!document.getElementById('uploadDialog').open&&!document.body.classList.contains('modal-open'));__assert(document.activeElement.id==='uploadToggle','Escape 未恢复焦点');__assertView('settings');return true;})()`)
	resize(390, 844)
	call("Emulation.setEmulatedMedia", map[string]any{"features": []map[string]string{{"name": "prefers-color-scheme", "value": "dark"}}})
	evaluate(`new Promise(resolve=>setTimeout(resolve,200))`)
	evaluate(`(()=>{canyouLayout.open('chat');document.activeElement.blur();document.getElementById('chatLog').scrollTop=0;__assert(matchMedia('(prefers-color-scheme:dark)').matches,'深色模拟失败');return true;})()`)
	shot("workspace-mobile-chat-dark")
	for _, size := range [][2]int{{390, 844}, {360, 640}} {
		resize(size[0], size[1])
		assertFileAlignment()
		evaluate(`document.getElementById('fileList').scrollTop=0;true`)
		shot(fmt.Sprintf("files-aligned-dark-%dx%d", size[0], size[1]))
	}
	evaluate(`canyouLayout.open('chat');true`)
	resize(1440, 900)
	shot("workspace-desktop-dark")
	for _, size := range [][2]int{{1100, 800}, {1024, 768}, {768, 1024}, {360, 640}, {844, 390}} {
		resize(size[0], size[1])
		evaluate(`(()=>{canyouLayout.open('files');__assert(document.documentElement.scrollWidth<=innerWidth,'深色文件横向溢出');canyouLayout.open('chat');__assert(document.getElementById('chatSend').getBoundingClientRect().bottom<=document.getElementById('chatPanel').getBoundingClientRect().bottom,'深色聊天输入裁切');return true;})()`)
		evaluate(`new Promise(resolve=>setTimeout(resolve,200))`)
		shot(fmt.Sprintf("workspace-dark-%dx%d", size[0], size[1]))
	}
	for _, size := range [][2]int{{1440, 900}, {1100, 800}, {1024, 768}, {768, 1024}, {390, 844}, {360, 640}, {844, 390}} {
		resize(size[0], size[1])
		for _, view := range []string{"devices", "settings"} {
			evaluate(fmt.Sprintf(`canyouLayout.open(%q);__assertView(%q);document.activeElement.blur();true`, view, view))
			shot(fmt.Sprintf("panels-%s-dark-%dx%d", view, size[0], size[1]))
		}
	}
	evaluate(`canyouLayout.open('chat');true`)
	if value := evaluate(`JSON.stringify(window.__uiErrors)`); string(value) != `"[]"` {
		t.Fatalf("浏览器异常: %s", value)
	}
	// 触控横屏使用相同布局，但输入保持 16px、Enter 换行。
	call("Emulation.setTouchEmulationEnabled", map[string]any{"enabled": true, "maxTouchPoints": 1})
	resize(844, 390)
	evaluate(`(()=>{__assert(matchMedia('(pointer:coarse)').matches,'触控模拟未启用');const text=document.getElementById('chatText');text.value='横屏触控草稿';text.dispatchEvent(new Event('input'));const e=new KeyboardEvent('keydown',{key:'Enter',cancelable:true,bubbles:true});text.dispatchEvent(e);__assert(!e.defaultPrevented&&parseFloat(getComputedStyle(text).fontSize)>=16,'触控换行或字号错误');__assert(document.getElementById('chatSend').getBoundingClientRect().bottom<=innerHeight,'触控横屏发送按钮裁切');text.value='';text.dispatchEvent(new Event('input'));return true;})()`)
	call("Emulation.setTouchEmulationEnabled", map[string]any{"enabled": false})
	// 仅模拟可视区域收缩来验证键盘分支，不代替真机软键盘验收。
	resize(390, 844)
	evaluate(`(()=>{const v=visualViewport,text=document.getElementById('chatText');text.focus();Object.defineProperty(v,'height',{configurable:true,value:320});Object.defineProperty(v,'offsetTop',{configurable:true,value:0});try{v.dispatchEvent(new Event('resize'));__assert(document.body.classList.contains('keyboard-open')&&getComputedStyle(document.querySelector('.view-nav')).display==='none','键盘出现时导航未隐藏');__assert(document.getElementById('chatSend').getBoundingClientRect().bottom<=320,'模拟键盘遮挡发送按钮');}finally{delete v.height;delete v.offsetTop;text.blur();v.dispatchEvent(new Event('resize'));}return true;})()`)
	// 刷新恢复目录与内页；独立保存宽窄状态，旧 chat 状态仍可迁移。
	reload := func() {
		t.Helper()
		call("Page.reload", map[string]any{"ignoreCache": true})
		time.Sleep(300 * time.Millisecond)
		evaluate(`(async()=>{await __wait(()=>document.getElementById('chatStatus')?.classList.contains('ok')&&document.getElementById('fileList')?.getAttribute('aria-busy')==='false');__assert(__uiErrors.length===0,'刷新产生脚本异常');return true;})()`)
	}
	resize(390, 844)
	evaluate(`(async()=>{canyouLayout.open('files');document.querySelector('[data-directory]').click();await __wait(()=>document.querySelector('[aria-current="page"]')?.textContent==='子目录');canyouLayout.open('chat');document.getElementById('chatSettingsToggle').click();document.getElementById('uploadToggle').click();sessionStorage.setItem('__didBefore',CANYOU_DID);return true;})()`)
	reload()
	evaluate(`__assert(document.querySelector('[aria-current="page"]').textContent==='子目录'&&!document.getElementById('settingsPanel').hidden&&document.getElementById('filesPanel').hidden&&document.getElementById('uploadDialog').open,'刷新未恢复目录、页面或上传窗口');__assert(CANYOU_DID===sessionStorage.getItem('__didBefore'),'刷新改变设备标识');document.getElementById('uploadDialog').close();true`)
	evaluate(`localStorage.setItem('canyou.ui.state',JSON.stringify({chat:false,dir:'',page:'chat',upload:false}));true`)
	resize(1440, 900)
	reload()
	evaluate(`__assert(document.getElementById('chatPanel').hidden&&canyouReadUI().desktopView==='files'&&canyouReadUI().compactView==='files','旧收起状态迁移失败');localStorage.setItem('canyou.ui.state',JSON.stringify({chat:true,dir:'',page:'chat',upload:false}));true`)
	resize(390, 844)
	reload()
	evaluate(`__assert(!document.getElementById('chatPanel').hidden&&canyouReadUI().desktopView==='chat'&&canyouReadUI().compactView==='chat','旧展开状态迁移失败');localStorage.removeItem('canyou.ui.state');true`)
	reload()
	evaluate(`__assert(!document.getElementById('filesPanel').hidden&&document.getElementById('chatPanel').hidden,'首次手机没有默认文件页');__assert([...document.querySelectorAll('[data-unread]')].every(e=>e.hidden),'历史同步计入了未读');true`)
	// 独立面板的刷新、旧页签迁移、新版本优先与宽窄屏状态。
	for _, view := range []string{"devices", "settings"} {
		for _, size := range [][2]int{{1440, 900}, {390, 844}} {
			resize(size[0], size[1])
			evaluate(fmt.Sprintf(`localStorage.setItem('canyou.ui.state',JSON.stringify({desktopChatOpen:true,compactView:'chat',page:%q,dir:'/',upload:false,custom:'保留'}));true`, view))
			reload()
			evaluate(fmt.Sprintf(`__assertView(%q);__assert(canyouReadUI().navigationVersion===2&&canyouReadUI().custom==='保留','迁移丢失其他状态');true`, view))
			reload()
			evaluate(fmt.Sprintf(`__assertView(%q);true`, view))
		}
	}
	resize(1440, 900)
	evaluate(`canyouLayout.open('devices');true`)
	resize(390, 844)
	evaluate(`canyouLayout.open('settings');true`)
	resize(1440, 900)
	evaluate(`__assertView('devices');true`)
	resize(390, 844)
	evaluate(`__assertView('settings');localStorage.setItem('canyou.ui.state',JSON.stringify({navigationVersion:2,desktopView:'files',compactView:'devices',page:'settings',desktopChatOpen:true}));true`)
	reload()
	evaluate(`__assertView('devices');true`)
	resize(1440, 900)
	evaluate(`__assertView('files');localStorage.setItem('canyou.ui.state',JSON.stringify({navigationVersion:2,desktopView:'invalid',compactView:null}));true`)
	reload()
	evaluate(`__assertView('chat');true`)
	resize(390, 844)
	evaluate(`__assertView('files');localStorage.setItem('canyou.ui.state','[]');true`)
	reload()
	evaluate(`__assertView('files');sessionStorage.setItem('__noUICache','1');true`)
	reload()
	evaluate(`canyouLayout.open('settings');__assertView('settings');canyouLayout.close();__assertView('files');__assert(__uiErrors.length===0&&__sockets.length===1,'缓存不可用阻断导航或重复连接');localStorage.removeItem('canyou.ui.state');true`)
	reload()
	evaluate(`__assertView('files');true`)
	// 主题偏好：真实接口落库，系统变化只改变展示，不发保存请求。
	emulateTheme := func(theme string) {
		call("Emulation.setEmulatedMedia", map[string]any{"features": []map[string]string{{"name": "prefers-color-scheme", "value": theme}}})
	}
	assertTheme := func(mode, actual string) {
		t.Helper()
		evaluate(fmt.Sprintf(`(async()=>{await __wait(()=>document.documentElement.dataset.theme===%q);const selected=document.querySelector('input[name="theme"]:checked');__assert(selected?.value===%q,'主题选项不一致');__assert(getComputedStyle(document.documentElement).colorScheme===%q,'原生控件主题不一致');__assert(getComputedStyle(document.body).backgroundColor===(%q==='dark'?'rgb(16, 20, 29)':'rgb(244, 246, 250)'),'页面颜色被系统样式覆盖');__assert(document.getElementById('themeColor').content===(%q==='dark'?'#10141d':'#f4f6fa'),'浏览器主题色不一致');return true;})()`, actual, mode, actual, actual, actual))
	}
	selectTheme := func(mode string) {
		t.Helper()
		evaluate(fmt.Sprintf(`(async()=>{document.querySelector('input[name="theme"][value="%s"]').click();await __wait(()=>!document.getElementById('themeOptions').disabled);__assert(document.querySelector('input[name="theme"]:checked').value===%q&&!document.getElementById('themeStatus').classList.contains('bad'),'主题保存失败');return true;})()`, mode, mode))
	}
	evaluate(`document.getElementById('chatSettingsToggle').click();__assert(__deviceReads===1&&__themePosts===0,'设备请求重复或初始化写入主题');true`)
	assertTheme("system", "dark")
	emulateTheme("light")
	assertTheme("system", "light")
	evaluate(`__assert(__themePosts===0,'系统变化触发了主题保存');true`)
	selectTheme("dark")
	assertTheme("dark", "dark")
	emulateTheme("dark")
	selectTheme("light")
	assertTheme("light", "light")
	evaluate(`__assert(__themePosts===2,'手动选择未按次保存');true`)
	selectTheme("system")
	assertTheme("system", "dark")
	emulateTheme("light")
	assertTheme("system", "light")
	evaluate(`__assert(__themePosts===3,'跟随系统重复写库');true`)
	// 切换主题不重建页面，不丢失选择、上传队列和聊天草稿。
	evaluate(`(()=>{canyouLayout.open('files');document.querySelector('.pick').click();window.__themePick=document.querySelector('.pick');document.getElementById('chatText').value='主题切换保留草稿';const dt=new DataTransfer();dt.items.add(new File(['主题上传'],'主题队列.txt'));const input=document.getElementById('fileInput');input.files=dt.files;input.dispatchEvent(new Event('change'));document.getElementById('chatSettingsToggle').click();window.__themeSockets=__sockets.length;return true;})()`)
	// 平级面板切换、关闭均保留草稿、未保存设置和各自滚动位置。
	evaluate(`(()=>{
  const el=id=>document.getElementById(id),name=el('chatName'),limit=el('chatLimit'),oldName=name.value,oldLimit=limit.value;
  name.value='尚未保存的名称';limit.value='177';const oldSwatch=document.querySelector('.chat-swatch[aria-pressed="true"]');
  document.querySelector('.chat-swatch[aria-pressed="false"]').click();const chosen=document.querySelector('.chat-swatch[aria-pressed="true"]');
  const page=el('chatPageSettings');page.scrollTop=page.scrollHeight;const scroll=page.scrollTop;__assert(scroll>0,'设置页未产生滚动样例');
  canyouLayout.open('devices');canyouLayout.close();canyouLayout.open('settings');
  __assert(page.scrollTop===scroll&&name.value==='尚未保存的名称'&&limit.value==='177'&&chosen.getAttribute('aria-pressed')==='true','切换丢失未提交设置或滚动位置');
  __assert(__themePick.checked&&el('chatText').value==='主题切换保留草稿'&&el('fileInput').files.length===1&&__sockets.length===__themeSockets,'切换丢失选择、草稿、上传或重建连接');
  name.value=oldName;limit.value=oldLimit;document.querySelector('[aria-label="'+oldSwatch.getAttribute('aria-label')+'"]').click();page.scrollTop=0;return true;
})()`)
	for _, size := range [][2]int{{1440, 900}, {1024, 768}, {390, 844}, {360, 640}} {
		resize(size[0], size[1])
		evaluate(`canyouLayout.open('settings');__assertView('settings');true`)
		for _, mode := range []string{"dark", "light"} {
			selectTheme(mode)
			assertTheme(mode, mode)
			evaluate(`(()=>{const page=document.getElementById('chatPageSettings');page.scrollTop=0;document.activeElement.blur();__assert(document.documentElement.scrollWidth<=innerWidth&&page.scrollWidth<=page.clientWidth,'主题设置横向溢出');for(const label of document.querySelectorAll('.theme-option')){const r=label.getBoundingClientRect();__assert(r.width>=44&&r.height>=44&&r.right<=innerWidth,'主题选项不可点或超出视口');}__assert(__themePick.checked&&document.getElementById('chatText').value==='主题切换保留草稿'&&document.getElementById('chips').children.length===1&&__sockets.length===__themeSockets,'主题切换丢失状态或重连');return true;})()`)
			shot(fmt.Sprintf("theme-%s-%dx%d", mode, size[0], size[1]))
		}
	}
	// 单选框原生方向键操作。
	resize(1440, 900)
	evaluate(`canyouLayout.open('settings');true`)
	selectTheme("dark")
	evaluate(`document.getElementById('themeDark').focus();true`)
	for _, mode := range []string{"light", "system"} {
		call("Input.dispatchKeyEvent", map[string]any{"type": "keyDown", "key": "ArrowRight", "code": "ArrowRight", "windowsVirtualKeyCode": 39})
		call("Input.dispatchKeyEvent", map[string]any{"type": "keyUp", "key": "ArrowRight", "code": "ArrowRight", "windowsVirtualKeyCode": 39})
		evaluate(fmt.Sprintf(`(async()=>{await __wait(()=>!document.getElementById('themeOptions').disabled);__assert(document.querySelector('input[name="theme"]:checked').value===%q&&document.activeElement.name==='theme'&&document.activeElement.matches(':focus-visible'),'主题连续方向键操作或焦点失败');return true;})()`, mode))
	}
	selectTheme("light")
	// 在途请求锁定及失败回退，随后可正常重试。
	evaluate(`(async()=>{const native=window.fetch;window.fetch=(url,options)=>url==='/gate/theme'?new Promise(resolve=>window.__failSave=()=>resolve(new Response(JSON.stringify({ok:false,message:'模拟保存失败'}),{status:500}))):native(url,options);document.getElementById('themeDark').click();await __wait(()=>!!window.__failSave);__assert(document.getElementById('themeOptions').disabled&&document.documentElement.dataset.theme==='dark','保存时未锁定或未预览');canyouTheme.save('system');window.__failSave();await __wait(()=>!document.getElementById('themeOptions').disabled);window.fetch=native;__assert(document.getElementById('themeLight').checked&&document.documentElement.dataset.theme==='light'&&document.getElementById('themeStatus').classList.contains('bad'),'失败未回退');__assert(localStorage.getItem('canyou.theme.'+CANYOU_DID)==='light','失败覆盖了已确认缓存');return true;})()`)
	selectTheme("dark")
	reload()
	assertTheme("dark", "dark")
	evaluate(`localStorage.removeItem('canyou.theme.'+CANYOU_DID);true`)
	reload()
	assertTheme("dark", "dark")
	// 读取在途时先用首屏缓存，禁止提交；返回数据库结果后校准。
	var themeDid string
	if err := json.Unmarshal(evaluate(`CANYOU_DID`), &themeDid); err != nil {
		t.Fatal(err)
	}
	navigateMain := func() {
		call("Page.navigate", map[string]any{"url": "http://127.0.0.1:18082/?did=" + themeDid})
		time.Sleep(300 * time.Millisecond)
	}
	evaluate(`sessionStorage.setItem('__holdDeviceRead','1');localStorage.setItem('canyou.theme.'+CANYOU_DID,'light');true`)
	navigateMain()
	evaluate(`(async()=>{await __wait(()=>!!window.__releaseDeviceRead&&!!document.getElementById('themeOptions'));__assert(document.documentElement.dataset.theme==='light'&&document.getElementById('themeOptions').disabled,'首屏缓存或读取锁定失败');const devices=document.getElementById('chatDevicesToggle');window.__deviceEntryPositions={};const r=devices.getBoundingClientRect();__assert(!devices.hidden&&devices.disabled&&r.width>0,'首屏设备入口未占位禁用');__deviceEntryPositions[innerWidth]=r.left;canyouTheme.save('system');__assert(__themePosts===0,'初始化前提交主题');return true;})()`)
	shot("devices-entry-loading-desktop")
	resize(390, 844)
	evaluate(`(()=>{const d=document.getElementById('chatDevicesToggle'),r=d.getBoundingClientRect();__assert(d.disabled&&!d.hidden&&r.width>=44&&r.height>=44,'手机首屏设备入口未占位');__deviceEntryPositions[innerWidth]=r.left;return true;})()`)
	shot("devices-entry-loading-mobile")
	evaluate(`(async()=>{window.__releaseDeviceRead();await canyouDeviceReady;await __wait(()=>!document.getElementById('chatDevicesToggle').disabled);const d=document.getElementById('chatDevicesToggle');__assert(!d.hidden&&Math.abs(d.getBoundingClientRect().left-__deviceEntryPositions[innerWidth])<1,'手机就绪时设备入口跳动');__assert(document.documentElement.dataset.theme==='dark'&&document.getElementById('themeDark').checked,'迟到读取没有校准主题');return true;})()`)
	resize(1440, 900)
	evaluate(`(()=>{const d=document.getElementById('chatDevicesToggle'),r=d.getBoundingClientRect();__assert(Math.abs(r.left-__deviceEntryPositions[innerWidth])<1,'桌面就绪时设备入口跳动：'+JSON.stringify({before:__deviceEntryPositions,after:r.left,width:r.width,label:d.textContent}));return true;})()`)
	// 缓存不可用、网络读取失败都不阻断页面和聊天。
	evaluate(`sessionStorage.setItem('__noThemeCache','1');true`)
	navigateMain()
	evaluate(`(async()=>{await canyouDeviceReady;await __wait(()=>document.getElementById('chatStatus').classList.contains('ok'));__assert(document.documentElement.dataset.theme==='dark'&&__uiErrors.length===0,'缓存不可用导致主题或聊天失败');return true;})()`)
	evaluate(`sessionStorage.setItem('__failDeviceRead','1');true`)
	navigateMain()
	evaluate(`(async()=>{await canyouDeviceReady;await __wait(()=>document.getElementById('chatStatus').classList.contains('ok'));__assert(document.documentElement.dataset.theme==='dark'&&!document.getElementById('themeOptions').disabled&&document.getElementById('themeStatus').classList.contains('bad'),'读取失败没有回退');return true;})()`)
	// 验证页先用缓存，解锁响应带回数据库主题后进入工作台。
	evaluate(`localStorage.setItem('canyou.theme.'+CANYOU_DID,'light');sessionStorage.setItem('__denyDeviceRead','1');true`)
	call("Page.navigate", map[string]any{"url": "http://127.0.0.1:18082/"})
	time.Sleep(300 * time.Millisecond)
	evaluate(`(async()=>{await __wait(()=>document.getElementById('gateForm')&&!document.getElementById('gateForm').hidden);__assert(document.documentElement.dataset.theme==='light','验证页未使用缓存主题');return true;})()`)
	shot("theme-gate-light")
	evaluate(`document.getElementById('gatePass').value='1234';document.getElementById('gateForm').requestSubmit();true`)
	time.Sleep(300 * time.Millisecond)
	evaluate(`(async()=>{await __wait(()=>document.getElementById('chatStatus')?.classList.contains('ok'));__assert(document.documentElement.dataset.theme==='dark'&&localStorage.getItem('canyou.theme.'+CANYOU_DID)==='dark','验证页解锁主题未衔接');return true;})()`)
	// 没有聊天模板的页面也独立读取并应用主题。
	call("Page.navigate", map[string]any{"url": "http://127.0.0.1:18082/?__test_files_only=1&did=" + themeDid})
	time.Sleep(300 * time.Millisecond)
	evaluate(`(async()=>{await __wait(()=>!!window.canyouDeviceReady);await canyouDeviceReady;await __wait(()=>document.querySelectorAll('.row').length>0);__assert(!document.getElementById('chatPanel')&&document.documentElement.dataset.theme==='dark'&&__deviceReads===1&&__sockets.length===0&&__uiErrors.length===0,'无聊天页面主题恢复失败');return true;})()`)
	navigateMain()
	evaluate(`(async()=>{await canyouDeviceReady;await __wait(()=>document.getElementById('chatStatus').classList.contains('ok'));document.getElementById('chatSettingsToggle').click();if(document.getElementById('settingsPanel').hidden)document.getElementById('chatSettingsToggle').click();return true;})()`)
	selectTheme("system")
	assertTheme("system", "light")
	evaluate(`__assert(__uiErrors.length===0,'主题回归产生页面异常');true`)
	t.Log("尺寸、目录、刷新迁移、选择、媒体播放释放、上传反馈、键盘、双设备同步及重连回归通过；软键盘仍需真机验收")
	t.Log("主题三选项、系统变化、持久化恢复、失败回退和无聊天页面回归通过")
	t.Log("平级导航、关闭与 Escape、旧状态迁移、窄屏高亮、未读及断线不跳页回归通过")
}

// newListingTestHandler 构造带根目录/子目录/文件的共享处理器（聊天关闭、允许上传）
func newListingTestHandler(t *testing.T) (http.Handler, string) {
	t.Helper()
	root := t.TempDir()
	sub := filepath.Join(root, "子目录")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	// 固定修改时间保证排序断言稳定：b 比 a 新
	for name, content := range map[string]string{"a.txt": "A", "b.txt": "BB"} {
		target := filepath.Join(root, name)
		if err := os.WriteFile(target, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		stamp := time.Date(2026, 11, 1, 10, 0, 0, 0, time.Local)
		if name == "b.txt" {
			stamp = stamp.Add(time.Hour)
		}
		if err := os.Chtimes(target, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	manager := newChatManager(filepath.Join(t.TempDir(), "chat.db"))
	return newShareHandler(root, t.TempDir(), manager), root
}

func postList(t *testing.T, handler http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/list", strings.NewReader(body))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func decodeList(t *testing.T, recorder *httptest.ResponseRecorder) listResponse {
	t.Helper()
	if recorder.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("响应不是 JSON: %q", recorder.Header().Get("Content-Type"))
	}
	var result listResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatalf("解析响应失败: %v body=%s", err, recorder.Body.String())
	}
	return result
}

// TestListAPIReturnsJSONListing 校验 /api/list 根目录与子目录的字段、排序与 parent
func TestListAPIReturnsJSONListing(t *testing.T) {
	handler, _ := newListingTestHandler(t)

	root := decodeList(t, postList(t, handler, `{"path":"/"}`))
	if !root.OK || root.Path != "/" || root.Parent != "" {
		t.Fatalf("根目录响应异常: %+v", root)
	}
	// 目录优先，其后按修改时间新到旧
	if len(root.Items) != 3 {
		t.Fatalf("根目录条目数错误: %+v", root.Items)
	}
	first := root.Items[0]
	if !first.IsDirectory || first.Name != "子目录" || first.Path != "/子目录" || first.Link != "/子目录/" || first.Icon != "📁" {
		t.Fatalf("目录条目字段错误: %+v", first)
	}
	if b, a := root.Items[1], root.Items[2]; b.Name != "b.txt" || a.Name != "a.txt" {
		t.Fatalf("文件排序错误: %+v", root.Items)
	}
	file := root.Items[1]
	if file.IsDirectory || file.Link != "/b.txt" || file.Size != "2 B" || file.Icon != "📄" || !strings.Contains(file.ModTime, "2026-11-01") {
		t.Fatalf("文件条目字段错误: %+v", file)
	}

	sub := decodeList(t, postList(t, handler, `{"path":"/子目录"}`))
	if !sub.OK || sub.Path != "/子目录" || sub.Parent != "/" {
		t.Fatalf("子目录响应异常: %+v", sub)
	}
	if len(sub.Items) != 0 {
		t.Fatalf("空子目录应返回空列表: %+v", sub.Items)
	}
	if !strings.Contains(postList(t, handler, `{"path":"子目录"}`).Body.String(), `"parent":"/"`) {
		t.Fatal("缺首斜杠的路径应被归一化为根下的子目录")
	}
}

// TestListAPIRejectsBadRequests 校验穿越归一、不存在路径、非法方法与坏请求体
func TestListAPIRejectsBadRequests(t *testing.T) {
	handler, _ := newListingTestHandler(t)

	// 穿越一律被 Clean 限制回 viewDir 内部：/../../ 归一为 /
	traversal := decodeList(t, postList(t, handler, `{"path":"/子目录/../../"}`))
	if !traversal.OK || traversal.Path != "/" {
		t.Fatalf("穿越路径未被限制在共享目录: %+v", traversal)
	}

	if recorder := postList(t, handler, `{"path":"/不存在"}`); recorder.Code != http.StatusNotFound {
		t.Fatalf("不存在目录应 404，实际 %d", recorder.Code)
	} else if result := decodeList(t, recorder); result.OK || result.Message == "" {
		t.Fatalf("404 响应体错误: %+v", result)
	}

	// 文件不是目录，同样 404
	if recorder := postList(t, handler, `{"path":"/a.txt"}`); recorder.Code != http.StatusNotFound {
		t.Fatalf("文件路径应 404，实际 %d", recorder.Code)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/list", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET 应 405，实际 %d", recorder.Code)
	}

	if recorder := postList(t, handler, `not-json`); recorder.Code != http.StatusBadRequest {
		t.Fatalf("坏请求体应 400，实际 %d", recorder.Code)
	}
}

// TestIndexShellIsDirectoryAgnostic 任意目录路径返回同一个单页壳，不再内嵌列表行
func TestIndexShellIsDirectoryAgnostic(t *testing.T) {
	handler, root := newListingTestHandler(t)
	if err := os.Mkdir(filepath.Join(root, "mydesktop"), 0o755); err != nil {
		t.Fatal(err)
	}

	fetch := func(target string) string {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, target, nil)
		request.Header.Set("Accept", "text/html")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s 应 200，实际 %d", target, recorder.Code)
		}
		return recorder.Body.String()
	}

	home := fetch("/")
	desktop := fetch("/mydesktop/?did=00000a5f9297600000ed86d40c")
	for name, body := range map[string]string{"根页": home, "旧目录页": desktop} {
		for _, marker := range []string{`id="pathTitle"`, `id="fileList"`, `'/api/list'`, `data-base="/"`, `kindOf`, `lightbox`, `lb-media`, `_blank`} {
			if !strings.Contains(body, marker) {
				t.Fatalf("%s 缺少壳标记 %s", name, marker)
			}
		}
		// 列表不再服务端渲染：文件名与 .. 行都不应出现在壳里
		for _, leaked := range []string{"a.txt", "b.txt", "子目录", ">..<"} {
			if strings.Contains(body, leaked) {
				t.Fatalf("%s 不应内嵌目录内容: %s", name, leaked)
			}
		}
		// 聊天关闭不渲染聊天按钮；允许上传则渲染上传按钮
		if strings.Contains(body, `id="chatToggle"`) {
			t.Fatalf("%s 聊天关闭时不应渲染 chatToggle", name)
		}
		if !strings.Contains(body, `id="uploadToggle"`) {
			t.Fatalf("%s 应渲染 uploadToggle", name)
		}
	}
	if home != desktop {
		t.Fatal("同一壳应与请求路径无关")
	}

	// 文件仍由 http.FileServer 走真实路径直接服务
	request := httptest.NewRequest(http.MethodGet, "/a.txt", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || recorder.Body.String() != "A" {
		t.Fatalf("文件直链行为回归: code=%d body=%q", recorder.Code, recorder.Body.String())
	}
}

// TestBuildVersionInjection 在临时目录构建最小 EXE，校验 JSON、资源属性及运行时版本，不改项目版本文件。
func TestBuildVersionInjection(t *testing.T) {
	for _, name := range []string{"cmd.exe", "powershell.exe", "go", "goversioninfo"} {
		if _, err := exec.LookPath(name); err != nil {
			t.Skipf("构建版本集成测试需要 %s", name)
		}
	}
	script, err := os.ReadFile("build.bat")
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := os.ReadFile("versioninfo.json")
	if err != nil {
		t.Fatal(err)
	}
	var template map[string]any
	if err := json.Unmarshal(metadata, &template); err != nil {
		t.Fatal(err)
	}
	template["IconPath"] = ""
	template["StringFileInfo"].(map[string]any)["LegalCopyright"] = "Copyright © 2001 ErWa"
	metadata, err = json.MarshalIndent(template, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, version, tag, label string
		parts                     [4]int
		invalid                   bool
	}{
		{name: "环境变量优先于标签", version: "v2.0.1", tag: "v1.0.1", label: "2.0.1.0", parts: [4]int{2, 0, 1, 0}},
		{name: "四段版本", version: "2.0.1.7", label: "2.0.1.7", parts: [4]int{2, 0, 1, 7}},
		{name: "预发布版本", version: "v2.0.1-beta.1+build.7", label: "2.0.1-beta.1+build.7", parts: [4]int{2, 0, 1, 0}},
		{name: "最近标签", tag: "v3.4.5", label: "3.4.5.0", parts: [4]int{3, 4, 5, 0}},
		{name: "无标签回退", label: "dev"},
		{name: "数字上限", version: "v65535.0.0", label: "65535.0.0.0", parts: [4]int{65535, 0, 0, 0}},
		{name: "非法格式", version: "v2.0.bad", invalid: true},
		{name: "版本溢出", version: "v65536.0.0", invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			gitStub := "@echo off\r\necho called>git-called.txt\r\nexit /b 1\r\n"
			if tc.tag != "" {
				gitStub = "@echo off\r\necho called>git-called.txt\r\necho " + tc.tag + "\r\nexit /b 0\r\n"
			}
			for name, content := range map[string][]byte{
				"git.cmd":          []byte(gitStub),
				"build.bat":        script,
				"versioninfo.json": metadata,
				"go.mod":           []byte("module buildfixture\n\ngo 1.22\n"),
				"main.go":          []byte("package main\nvar Version = \"dev\"\nfunc main() { println(Version) }\n"),
			} {
				if err := os.WriteFile(filepath.Join(dir, name), content, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			run := func(name string, args ...string) ([]byte, error) {
				t.Helper()
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				defer cancel()
				command := exec.CommandContext(ctx, name, args...)
				command.Dir = dir
				command.Env = append(os.Environ(), "VER="+tc.version, "GOOS=windows", "GOARCH=amd64", "GOWORK=off", "GOFLAGS=")
				return command.CombinedOutput()
			}
			mustRun := func(name string, args ...string) []byte {
				t.Helper()
				output, err := run(name, args...)
				if err != nil {
					t.Fatalf("%s 执行失败: %v\n%s", name, err, output)
				}
				return output
			}
			startYear := time.Now().Year()
			output, buildErr := run("cmd.exe", "/d", "/c", "build.bat")
			endYear := time.Now().Year()
			_, gitCalledErr := os.Stat(filepath.Join(dir, "git-called.txt"))
			if (tc.version == "" && gitCalledErr != nil) || (tc.version != "" && !os.IsNotExist(gitCalledErr)) {
				t.Fatalf("VER 与 Git 标签优先级错误: %v", gitCalledErr)
			}
			stamped, err := os.ReadFile(filepath.Join(dir, "versioninfo.json"))
			if err != nil {
				t.Fatal(err)
			}
			if tc.invalid {
				if buildErr == nil || !bytes.Equal(stamped, metadata) {
					t.Fatalf("非法版本应中止构建且不改写元数据: %v\n%s", buildErr, output)
				}
				if _, err := os.Stat(filepath.Join(dir, "resource.syso")); !os.IsNotExist(err) {
					t.Fatalf("非法版本不应生成资源: %v", err)
				}
				return
			}
			if buildErr != nil {
				t.Fatalf("版本构建失败: %v\n%s", buildErr, output)
			}
			var info struct {
				FixedFileInfo struct {
					FileVersion, ProductVersion map[string]int
				}
				StringFileInfo map[string]string
			}
			if err := json.Unmarshal(stamped, &info); err != nil {
				t.Fatalf("版本 JSON 无法解析（应为无 BOM 的 UTF-8）: %v", err)
			}
			for key, parts := range map[string]map[string]int{
				"FileVersion": info.FixedFileInfo.FileVersion, "ProductVersion": info.FixedFileInfo.ProductVersion,
			} {
				if info.StringFileInfo[key] != tc.label {
					t.Fatalf("%s 文字版本错误: %q", key, info.StringFileInfo[key])
				}
				for i, part := range []string{"Major", "Minor", "Patch", "Build"} {
					if parts[part] != tc.parts[i] {
						t.Fatalf("%s.%s 数字版本错误: %v", key, part, parts)
					}
				}
			}
			copyright := info.StringFileInfo["LegalCopyright"]
			if copyright != fmt.Sprintf("Copyright © %d ErWa", startYear) && copyright != fmt.Sprintf("Copyright © %d ErWa", endYear) {
				t.Fatalf("版权年份未取构建时的当前年份，或版权文字被改动: %q", copyright)
			}
			for key, value := range template["StringFileInfo"].(map[string]any) {
				if key != "FileVersion" && key != "ProductVersion" && key != "LegalCopyright" && info.StringFileInfo[key] != value {
					t.Fatalf("非构建字段被改动: %s", key)
				}
			}
			version := tc.version
			if version == "" {
				version = tc.tag
			}
			if version == "" {
				version = "dev"
			}
			executable := "Canyou-" + version + ".exe"
			runtimeVersion := mustRun(filepath.Join(dir, executable))
			if strings.TrimSpace(string(runtimeVersion)) != version {
				t.Fatalf("程序运行时版本未正确注入: %s", runtimeVersion)
			}
			properties := mustRun("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "[Console]::OutputEncoding = New-Object Text.UTF8Encoding($false); (Get-Item -LiteralPath './"+executable+"').VersionInfo | Select-Object LegalCopyright,FileVersion,ProductVersion,FileMajorPart,FileMinorPart,FileBuildPart,FilePrivatePart,ProductMajorPart,ProductMinorPart,ProductBuildPart,ProductPrivatePart | ConvertTo-Json -Compress")
			var resource map[string]any
			if err := json.Unmarshal(properties, &resource); err != nil {
				t.Fatalf("无法读取 EXE 版本属性: %v\n%s", err, properties)
			}
			if resource["LegalCopyright"] != copyright {
				t.Fatalf("EXE 版权未使用新年份: %v", resource["LegalCopyright"])
			}
			for _, kind := range []string{"File", "Product"} {
				if resource[kind+"Version"] != tc.label {
					t.Fatalf("EXE %sVersion 未使用新版本: %v", kind, resource)
				}
				for i, part := range []string{"Major", "Minor", "Build", "Private"} {
					if resource[kind+part+"Part"] != float64(tc.parts[i]) {
						t.Fatalf("EXE 数字版本不匹配: %v", resource)
					}
				}
			}
		})
	}
}
