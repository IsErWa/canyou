package webui

import (
	"bytes"
	"context"
	"encoding/json"
	"html"
	"io/fs"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestEmbeddedScriptsAndWhitelist(t *testing.T) {
	entries, err := fs.Glob(resources, "scripts/*.js")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(scripts) {
		t.Fatal("嵌入脚本与白名单不一致")
	}
	for _, name := range entries {
		t.Run(name, func(t *testing.T) {
			data, err := resources.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "{{") {
				t.Fatal("独立脚本中仍有 Go 模板表达式")
			}
			assertInlineScripts(t, "<script>"+string(data)+"</script>")
		})
	}
	for _, name := range []string{"../gate.css", "styles/theme.css", "/theme.css", "missing.js", "templates/gate.html"} {
		if _, err := style(name); err == nil {
			t.Fatalf("样式白名单放行了 %q", name)
		}
		if _, err := script(name); err == nil {
			t.Fatalf("脚本白名单放行了 %q", name)
		}
	}
}

func TestResourceOrder(t *testing.T) {
	var body bytes.Buffer
	if err := RenderFiles(&body, FilesData{CanChat: true, CanUpload: true}); err != nil {
		t.Fatal(err)
	}
	page := body.String()
	blocks := regexp.MustCompile(`(?s)<script>(.*?)</script>`).FindAllStringSubmatch(page, -1)
	expected := []string{"theme-bootstrap.js", "identity.js", "theme.js", "workspace.js", "files.js", "upload-dialog.js", "upload.js", "chat.js"}
	if len(blocks) != len(expected) {
		t.Fatalf("脚本数量变化：%d", len(blocks))
	}
	for i, name := range expected {
		if blocks[i][1] != scripts[name] {
			t.Fatalf("脚本顺序变化：第 %d 项应为 %s", i, name)
		}
	}
	css := regexp.MustCompile(`(?s)<style>(.*?)</style>`).FindStringSubmatch(page)
	if len(css) != 2 || css[1] != styles["theme.css"]+styles["base.css"]+styles["panels.css"]+styles["workspace.css"] {
		t.Fatal("CSS 级联顺序变化")
	}
	body.Reset()
	if err := RenderGate(&body, "/", "dev"); err != nil {
		t.Fatal(err)
	}
	page = body.String()
	css = regexp.MustCompile(`(?s)<style>(.*?)</style>`).FindStringSubmatch(page)
	if len(css) != 2 || css[1] != styles["theme.css"]+styles["gate.css"] {
		t.Fatal("门禁样式顺序变化")
	}
	blocks = regexp.MustCompile(`(?s)<script>(.*?)</script>`).FindAllStringSubmatch(page, -1)
	for i, name := range []string{"theme-bootstrap.js", "identity.js", "gate.js"} {
		if len(blocks) != 3 || blocks[i][1] != scripts[name] {
			t.Fatalf("门禁脚本顺序变化：%s", name)
		}
	}
}

func TestGateInitializationEscaping(t *testing.T) {
	for _, target := range []string{"/a/b?q=%E4%B8%AD", "/目录/?x=1&y=2", "//evil.example/", "/</script><script>alert(1)</script>", "/a\"b", "/a\nb", "/x?value=\u2028\u2029"} {
		t.Run(target, func(t *testing.T) {
			var body bytes.Buffer
			if err := RenderGate(&body, target, "dev"); err != nil {
				t.Fatal(err)
			}
			html := body.String()
			init := regexp.MustCompile(`(?s)<script type="application/json" id="gateInit">(.*?)</script>`).FindStringSubmatch(html)
			if len(init) != 2 {
				t.Fatal("缺少安全初始化数据")
			}
			var data struct {
				Next string `json:"next"`
			}
			if err := json.Unmarshal([]byte(init[1]), &data); err != nil {
				t.Fatal(err)
			}
			if data.Next != safeInternalTarget(target) || strings.Contains(init[1], "<") {
				t.Fatalf("跳转地址转义失败：%q", init[1])
			}
			assertUniquePageIDs(t, html)
			assertInlineScripts(t, html)
		})
	}
	if got := jsonString("</script>"); strings.Contains(got, "<") {
		t.Fatalf("JSON 未转义标签：%s", got)
	}
}

func TestFilePageDataEscaping(t *testing.T) {
	var body bytes.Buffer
	attack := `"><script>alert('设备名')</script><img src=x onerror=alert(1)>`
	if err := RenderFiles(&body, FilesData{CanChat: true, CanUpload: true, ClientIP: attack}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body.String(), attack) || strings.Contains(body.String(), "ZgotmplZ") {
		t.Fatal("页面数据未按 HTML 上下文安全转义")
	}
	if !strings.Contains(body.String(), "&lt;script&gt;") {
		t.Fatal("没有输出转义后的文本")
	}
	assertInlineScripts(t, body.String())
}

// TestDeviceAuthorizationScripts 执行真实嵌入脚本，校验检查、自动登记和密码提交的请求顺序。
func TestDeviceAuthorizationScripts(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("前端授权流程测试需要 Node.js")
	}
	data, err := json.Marshal(map[string]string{
		"bootstrap": scripts["theme-bootstrap.js"],
		"files":     scripts["theme.js"],
		"gate":      scripts["gate.js"],
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, node, "-")
	command.Stdin = strings.NewReader("const scripts=" + string(data) + `;
const vm=require('node:vm'), assert=require('node:assert/strict');
const allowed={ok:true,allowed:true,id:7,did:'abababababababab',name:'数据库名称',theme:'dark'};
const missing=password=>({ok:false,allowed:false,requiresPassword:password});
const reply=(path,data,status=200)=>({path,data,status});
const check=data=>reply('/gate/check',data), unlock=data=>reply('/gate/unlock',data);
const flush=()=>new Promise(resolve=>setImmediate(resolve));
async function run(page,responses,submissions=[]){
  const calls=[],redirects=[],cache=new Map(),attrs={};
  const elements={
    gateInit:{textContent:JSON.stringify({next:'/目录/?x=1'})},
    gateNote:{hidden:false},gateForm:{hidden:true,addEventListener(type,fn){this[type]=fn;}},
    gatePass:{value:'',focus(){}},gateBtn:{disabled:false},gateError:{hidden:true,textContent:''},
    themeStatus:{textContent:'',classList:{toggle(){}}}
  };
  const sandbox={
    console,Promise,AbortController,setTimeout,clearTimeout,
    CANYOU_DID:allowed.did,canyouDeviceName:()=> '客户端名称',canyouWithDid:href=>href+'#identity',
    matchMedia:()=>({matches:false,addEventListener(){}}),
    localStorage:{getItem:key=>cache.get(key)||null,setItem:(key,value)=>cache.set(key,value)},
    document:{getElementById:id=>elements[id]||null,documentElement:{style:{},setAttribute:(key,value)=>attrs[key]=value}},
    location:{pathname:'/目录/',search:'?x=1',hash:'',replace:href=>redirects.push(href)},
    fetch:async(path,options)=>{
      calls.push({path,body:JSON.parse(options.body)});
      const expected=responses.shift();
      assert.ok(expected,'出现多余请求：'+path);assert.equal(path,expected.path);
      if(expected.network)throw new Error('模拟网络失败');
      return {ok:expected.status===200,json:async()=>expected.data};
    }
  };
  sandbox.window=sandbox;vm.createContext(sandbox);
  vm.runInContext(scripts.bootstrap,sandbox);vm.runInContext(scripts[page],sandbox);
  let device;
  if(page==='files')device=await sandbox.canyouDeviceReady;
  await flush();
  for(const password of submissions){elements.gatePass.value=password;elements.gateForm.submit({preventDefault(){}});await flush();}
  assert.equal(responses.length,0,'预期请求没有执行');
  assert.equal(calls[0].path,'/gate/check');assert.deepEqual(calls[0].body,{did:allowed.did});
  return {calls,redirects,cache,attrs,elements,device};
}
(async()=>{
  for(const page of ['files','gate']){
    let result=await run(page,[check(allowed)]);
    assert.equal(result.calls.length,1,'已有设备不应重复解锁');
    assert.equal(result.attrs['data-theme'],'dark');
    assert.equal(result.cache.get('canyou.theme.'+allowed.did),'dark');
    if(page==='files')assert.equal(result.device.name,allowed.name);
    else assert.equal(result.redirects.length,1);

    result=await run(page,[check(missing(false)),unlock(allowed)]);
    assert.equal(result.calls.length,2);assert.equal(result.calls[1].body.name,'客户端名称');
    assert.equal(result.calls[1].body.did,allowed.did);assert.ok(!result.calls[1].body.password);
    if(page==='files')assert.equal(result.device.id,7);
    else assert.equal(result.redirects.length,1);

    result=await run(page,[check(missing(true))]);
    assert.equal(result.calls.length,1,'有密码时不得自动提交 unlock');
    if(page==='files'){assert.equal(result.device,null);assert.equal(result.redirects.length,1);}
    else {assert.equal(result.redirects.length,0);assert.equal(result.elements.gateForm.hidden,false);}

    for(const response of [reply('/gate/check',{ok:false,allowed:false,message:'读取失败'},500),{path:'/gate/check',network:true}]){
      result=await run(page,[response]);
      assert.equal(result.calls.length,1,'读取失败不得自动登记');assert.equal(result.redirects.length,0);
      if(page==='files')assert.equal(result.device,null);
      else assert.equal(result.elements.gateError.hidden,false);
    }
    for(const status of [403,500]){
      result=await run(page,[check(missing(false)),reply('/gate/unlock',{ok:false,allowed:false,message:'授权失败'},status)]);
      assert.equal(result.redirects.length,0,'授权失败不得进入');
      assert.equal(result.cache.has('canyou.theme.'+allowed.did),false);
      if(page==='files')assert.equal(result.device,null);
      else assert.equal(result.elements.gateError.textContent,'授权失败');
    }
  }
  let result=await run('gate',[check(missing(true)),reply('/gate/unlock',{ok:false,message:'密码错误，请重试'},403)],['wrong']);
  assert.equal(result.redirects.length,0);assert.equal(result.elements.gateError.textContent,'密码错误，请重试');
  assert.equal(result.elements.gateBtn.disabled,false,'错误后应允许重试');
  result=await run('gate',[check(missing(true)),reply('/gate/unlock',{ok:false,message:'密码错误'},403),unlock(allowed)],['wrong','correct']);
  assert.equal(result.calls[2].body.password,'correct');assert.equal(result.redirects.length,1);
  result=await run('files',[check({ok:false,allowed:false})]);
  assert.equal(result.device,null);assert.equal(result.calls.length,1,'缺少密码状态时不得猜测自动授权');
  console.log('前端授权流程测试通过');
})().catch(error=>{console.error(error);process.exitCode=1;});
`)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("前端授权流程失败: %v\n%s", err, output)
	}
}

func TestServerVersionRendering(t *testing.T) {
	for _, test := range []struct{ version, want string }{
		{"v2.0.0", "v2.0.0"},
		{"v2.0.1-beta.1+build.7", "v2.0.1-beta.1+build.7"},
		{"dev", "dev"},
		{"", "dev"},
		{`<script>alert('x')</script>&`, `<script>alert('x')</script>&`},
	} {
		t.Run(test.version, func(t *testing.T) {
			var page bytes.Buffer
			if err := RenderFiles(&page, FilesData{Version: test.version}); err != nil {
				t.Fatal(err)
			}
			text := regexp.MustCompile(`<div class="sub">([^<]*)</div>`).FindStringSubmatch(page.String())
			if len(text) != 2 || html.UnescapeString(text[1]) != "在同一网络，轻松分享 · "+test.want {
				t.Fatal("文件页未安全显示服务器版本")
			}
			assertInlineScripts(t, page.String())
			page.Reset()
			if err := RenderGate(&page, "/", test.version); err != nil {
				t.Fatal(err)
			}
			text = regexp.MustCompile(`<p class="brand">([^<]*)</p>`).FindStringSubmatch(page.String())
			if len(text) != 2 || html.UnescapeString(text[1]) != "Canyou 共享 · "+test.want {
				t.Fatal("门禁页未安全显示服务器版本")
			}
			assertInlineScripts(t, page.String())
		})
	}
}
