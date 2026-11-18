package main

// chatTemplates 独立组织聊天界面，仍内嵌到最终单文件程序中。
const chatTemplates = `
{{define "darkThemeTokens"}}
color-scheme:dark;--bg:#10141d;--card:#191f2c;--text:#e7ecf5;--muted:#a0adc2;
--border:#2b3548;--accent:#91a7ff;--accent-ink:#121a32;--accent-soft:#263251;
--ok:#6ed9a0;--bad:#ff8b96;--shadow:none;
{{end}}
{{define "themeStyles"}}
:root{
  color-scheme:light;--bg:#f4f6fa;--card:#fff;--text:#202b3d;--muted:#657389;
  --border:#e2e7ef;--accent:#4263eb;--accent-ink:#fff;--accent-soft:#edf1ff;
  --ok:#15804b;--bad:#ce3546;--shadow:0 2px 6px rgba(24,39,75,.03);
}
@media(prefers-color-scheme:dark){:root:not([data-theme]){ {{template "darkThemeTokens"}} }}
:root[data-theme="dark"]{ {{template "darkThemeTokens"}} }
{{end}}
{{define "themeBootstrap"}}
<script>
(function(){
  var system=window.matchMedia('(prefers-color-scheme: dark)');
  var mode='system', confirmed='system', loaded=false, saving=false, note='', failed=false;
  function valid(value){return value==='dark'||value==='light'||value==='system';}
  function cacheKey(){
    try{var did=window.CANYOU_DID||localStorage.getItem('canyou.device.id')||'';return /^[0-9a-f]{8,64}$/.test(did)?'canyou.theme.'+did:'';}catch(e){return '';}
  }
  try{var key=cacheKey(), cached=key&&localStorage.getItem(key);if(valid(cached))mode=confirmed=cached;}catch(e){}
  function cache(){try{var key=cacheKey();if(key)localStorage.setItem(key,confirmed);}catch(e){}}
  function render(){
    var actual=mode==='system'?(system.matches?'dark':'light'):mode;
    document.documentElement.setAttribute('data-theme',actual);
    document.documentElement.style.colorScheme=actual;
    var meta=document.getElementById('themeColor');if(meta)meta.content=actual==='dark'?'#10141d':'#f4f6fa';
    var group=document.getElementById('themeOptions');
    if(group){group.disabled=!loaded||saving;group.querySelectorAll('input').forEach(function(input){input.checked=input.value===mode;});}
    var status=document.getElementById('themeStatus');
    if(status){status.textContent=note||(!loaded?'正在读取主题…':(mode==='system'?'当前跟随系统'+(actual==='dark'?'深色':'浅色')+'模式':'仅对本设备生效'));status.classList.toggle('bad',failed);}
  }
  function accept(value){mode=confirmed=valid(value)?value:'system';cache();render();}
  function request(path,body){
    var controller=new AbortController(), timer=setTimeout(function(){controller.abort();},10000);
    return Promise.resolve().then(function(){
      return fetch(path,{method:'post',cache:'no-store',signal:controller.signal,headers:{'Content-Type':'application/json','X-Device-ID':window.CANYOU_DID||''},body:JSON.stringify(body)});
    }).then(function(r){return r.json().then(function(data){if(!r.ok||!data.ok)throw new Error(data.message||'请求失败');return data;});})
      .finally(function(){clearTimeout(timer);});
  }
  function save(next){
    if(!loaded||saving||!valid(next)||next===confirmed)return;
    var trigger=document.activeElement;
    mode=next;saving=true;failed=false;note='保存中…';render();
    request('/gate/theme',{did:window.CANYOU_DID||'',theme:next,name:window.canyouDeviceName?canyouDeviceName():''})
      .then(function(data){if(data.theme!==next)throw new Error('服务器返回了不同的主题');confirmed=mode=next;cache();note='已保存，仅对本设备生效';})
      .catch(function(){mode=confirmed;failed=true;note='主题保存失败，已恢复原设置，请重新选择重试';})
      .finally(function(){
        saving=false;render();
        if(trigger && trigger.name==='theme' && trigger.getClientRects().length && document.activeElement===document.body)trigger.focus({preventScroll:true});
      });
  }
  window.canyouTheme={accept:accept,request:request,save:save,render:render,ready:function(ok){loaded=true;if(!ok){failed=true;note='主题读取失败，暂用本地或系统设置';}render();}};
  system.addEventListener('change',function(){if(mode==='system')render();});
  render();
})();
</script>
{{end}}
{{define "themeInit"}}
<script>
(function(){
  var theme=window.canyouTheme, group=document.getElementById('themeOptions');
  if(group)group.addEventListener('change',function(e){if(e.target.name==='theme')theme.save(e.target.value);});
  theme.render();
  // 与聊天共享设备信息请求；主题恢复不依赖聊天模板或 WebSocket。
  window.canyouDeviceReady=theme.request('/gate/check',{did:window.CANYOU_DID||'',name:window.canyouDeviceName?canyouDeviceName():''})
    .then(function(data){if(!data.allowed)throw new Error('设备尚未授权');theme.accept(data.theme);return data;})
    .catch(function(){return null;})
    .then(function(data){theme.ready(!!data);return data;});
})();
</script>
{{end}}
{{define "uiIcons"}}
<svg xmlns="http://www.w3.org/2000/svg" width="0" height="0" style="position:absolute;overflow:hidden" aria-hidden="true"><defs>
<symbol id="i-folder" viewBox="0 0 24 24"><path d="M3 7V5a2 2 0 0 1 2-2h5l3 3h6a2 2 0 0 1 2 2v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7Z"/><path d="M3 9h18"/></symbol>
<symbol id="i-file" viewBox="0 0 24 24"><path d="M14 3H5v18h14V8l-5-5Z M14 3v6h5 M8 13h8 M8 17h5"/></symbol>
<symbol id="i-upload" viewBox="0 0 24 24"><path d="M12 16V3m-5 5 5-5 5 5 M4 15v6h16v-6"/></symbol>
<symbol id="i-download" viewBox="0 0 24 24"><path d="M12 3v13m-5-5 5 5 5-5 M4 17v4h16v-4"/></symbol>
<symbol id="i-chat" viewBox="0 0 24 24"><path d="M21 11a9 9 0 0 1-9 9H4l-2 2V11a9 9 0 0 1 19 0Z M7 10h10 M7 14h6"/></symbol>
<symbol id="i-back" viewBox="0 0 24 24"><path d="m10 5-7 7 7 7 M3 12h18"/></symbol>
<symbol id="i-close" viewBox="0 0 24 24"><path d="m6 6 12 12 M18 6 6 18"/></symbol>
<symbol id="i-prev" viewBox="0 0 24 24"><path d="m15 5-7 7 7 7"/></symbol>
<symbol id="i-next" viewBox="0 0 24 24"><path d="m9 5 7 7-7 7"/></symbol>
<symbol id="i-users" viewBox="0 0 24 24"><circle cx="9" cy="7" r="3"/><path d="M3 21v-4a6 6 0 0 1 12 0v4 M16 4a3 3 0 0 1 0 6 M18 14a5 5 0 0 1 3 5v2"/></symbol>
<symbol id="i-settings" viewBox="0 0 24 24"><path d="M4 6h16 M4 12h16 M4 18h16 M8 3v6 M16 9v6 M10 15v6"/></symbol>
<symbol id="i-lock" viewBox="0 0 24 24"><rect x="4" y="10" width="16" height="11" rx="2"/><path d="M8 10V6a4 4 0 0 1 8 0v4 M12 14v3"/></symbol>
</defs></svg>
{{end}}
{{define "workspaceScript"}}
<script>
(function(){
  window.canyouIcon=function(name){
    var ns='http://www.w3.org/2000/svg', svg=document.createElementNS(ns,'svg'), use=document.createElementNS(ns,'use');
    svg.setAttribute('class','icon');svg.setAttribute('aria-hidden','true');use.setAttribute('href','#i-'+name);svg.appendChild(use);return svg;
  };
  // 原生 dialog 提供模态焦点约束，关闭后恢复到仍可见的触发控件。
  window.canyouDialog=function(dialog){
    var trigger=null;
    dialog.canyouOpen=function(){
      if(dialog.open) return;
      trigger=document.activeElement;dialog.showModal();document.body.classList.add('modal-open');
      document.dispatchEvent(new Event('canyou:visibility'));
    };
    dialog.addEventListener('close',function(){
      if(dialog.open) return;
      document.body.classList.toggle('modal-open',!!document.querySelector('dialog[open]'));
      if(trigger && trigger.isConnected && trigger.getClientRects().length) trigger.focus({preventScroll:true});
      document.dispatchEvent(new Event('canyou:visibility'));
    });
  };
  var saved=window.canyouReadUI(), wide=window.matchMedia('(min-width:1100px)');
  function validView(view){return ['files','chat','devices','settings'].includes(view);}
  var legacyPage=['chat','devices','settings'].includes(saved.page) ? saved.page : 'chat';
  var legacyDesktop=typeof saved.desktopChatOpen==='boolean' ? saved.desktopChatOpen : (typeof saved.chat==='boolean' ? saved.chat : true);
  var legacyCompact=saved.compactView==='chat'||saved.compactView==='files' ? saved.compactView==='chat' : saved.chat===true;
  var desktop=saved.navigationVersion===2 ? (validView(saved.desktopView) ? saved.desktopView : 'chat') : (legacyDesktop ? legacyPage : 'files');
  var compact=saved.navigationVersion===2 ? (validView(saved.compactView) ? saved.compactView : 'files') : (legacyCompact ? legacyPage : 'files');
  var files=document.getElementById('filesPanel'), list=document.getElementById('fileList');
  var panels={chat:document.getElementById('chatPanel'),devices:document.getElementById('devicesPanel'),settings:document.getElementById('settingsPanel')};
  var entries={chat:document.getElementById('chatToggle'),devices:document.getElementById('chatDevicesToggle'),settings:document.getElementById('chatSettingsToggle')};
  var filesTab=document.getElementById('filesTab'), chatTab=document.getElementById('chatTab');
  var fileScroll=0, scrollPositions={};
  function currentView(){var view=wide.matches ? desktop : compact;return view==='files'||panels[view] ? view : 'files';}
  function persist(){window.canyouUI({navigationVersion:2,desktopView:desktop,compactView:compact});}
  function apply(){
    document.dispatchEvent(new Event('canyou:beforelayout'));
    if(!files.hidden) fileScroll=list.scrollTop;
    Object.keys(panels).forEach(function(view){
      var panel=panels[view];if(panel&&!panel.hidden)scrollPositions[view]=panel.querySelector('.panel-page').scrollTop;
    });
    var view=currentView(), open=view!=='files';
    files.hidden=!wide.matches && open;
    Object.keys(panels).forEach(function(key){
      var panel=panels[key];
      if(panel){panel.hidden=key!==view;if(!panel.hidden)panel.querySelector('.panel-page').scrollTop=scrollPositions[key]||0;}
      if(entries[key])entries[key].setAttribute('aria-expanded',String(key===view));
    });
    document.body.classList.toggle('panel-open',open);
    if(filesTab) filesTab.setAttribute('aria-pressed',String(view==='files'));
    if(chatTab) chatTab.setAttribute('aria-pressed',String(view==='chat'));
    if(!files.hidden) list.scrollTop=fileScroll;
    document.dispatchEvent(new Event('canyou:layout'));
  }
  function focusEntry(view){
    var entry=view==='chat'&&!wide.matches ? chatTab : entries[view];
    if(!entry||entry.disabled||!entry.getClientRects().length)entry=filesTab;
    if(!entry||!entry.getClientRects().length)entry=document.getElementById('filesTitle');
    if(entry)entry.focus({preventScroll:true});
  }
  window.canyouLayout={
    open:function(view){
      if(!validView(view)||(view!=='files'&&!panels[view]))return;
      if(wide.matches)desktop=view;else compact=view;
      persist();apply();
    },
    close:function(){var view=currentView();this.open('files');focusEntry(view);},
    currentView:currentView
  };
  Object.keys(entries).forEach(function(view){
    if(entries[view])entries[view].addEventListener('click',function(){
      if(currentView()===view)window.canyouLayout.close();else window.canyouLayout.open(view);
    });
  });
  document.querySelectorAll('[data-panel-close]').forEach(function(button){button.addEventListener('click',function(){window.canyouLayout.close();});});
  document.addEventListener('keydown',function(e){
    if(e.key==='Escape'&&!e.isComposing&&!e.defaultPrevented&&!document.querySelector('dialog[open]')&&currentView()!=='files'){
      e.preventDefault();window.canyouLayout.close();
    }
  });
  if(filesTab)filesTab.addEventListener('click',function(){window.canyouLayout.open('files');});
  if(chatTab)chatTab.addEventListener('click',function(){window.canyouLayout.open('chat');});
  wide.addEventListener('change',apply);
  persist();apply();
  function viewport(){
    var v=window.visualViewport, wrap=document.querySelector('.wrap');
    var editing=/^(INPUT|TEXTAREA)$/.test(document.activeElement.tagName);
    var keyboard=editing && v && window.innerHeight-v.height>120;
    document.body.classList.toggle('keyboard-open',!!keyboard);
    if(v && window.innerWidth<1100){wrap.style.height=v.height+'px';wrap.style.transform='translateY('+v.offsetTop+'px)';}
    else {wrap.style.height='';wrap.style.transform='';}
  }
  if(window.visualViewport){visualViewport.addEventListener('resize',viewport);visualViewport.addEventListener('scroll',viewport);}
  window.addEventListener('resize',viewport);document.addEventListener('focusin',viewport);document.addEventListener('focusout',function(){setTimeout(viewport,0);});viewport();
})();
</script>
{{end}}
{{define "identityScript"}}
<script>
(function(){
  // 设备唯一标识：进页面先读 localStorage，有合法值直接沿用；没有就现算指纹并缓存回 localStorage。
  // 落盘后身份不再受系统/浏览器更新导致的指纹漂移影响；存储被禁时退回每次现算。
  // 非安全上下文（http://IP）没有 crypto.subtle，改用双种子 cyrb53 拼 26 位 hex。
  var fpCache='';
  function gpuInfo(){
    try{
      var c=document.createElement('canvas'); var gl=c.getContext('webgl')||c.getContext('experimental-webgl');
      if(!gl) return 'na';
      var ext=gl.getExtension('WEBGL_debug_renderer_info');
      var v=gl.getParameter(gl.VERSION)+'|'+(ext ? gl.getParameter(ext.UNMASKED_RENDERER_WEBGL) : gl.getParameter(gl.RENDERER));
      // 用完主动归还上下文，避免 iOS 累计创建超限后 getContext 返回 null。
      var lose=gl.getExtension('WEBGL_lose_context'); if(lose) lose.loseContext();
      return v;
    }catch(e){ return 'na'; }
  }
  function cyrb53(str,seed){ var h1=0xdeadbeef^seed,h2=0x41c6ce57^seed; for(var i=0;i<str.length;i++){ var ch=str.charCodeAt(i); h1=Math.imul(h1^ch,2654435761); h2=Math.imul(h2^ch,1597334677); } h1=Math.imul(h1^(h1>>>16),2246822507)^Math.imul(h2^(h2>>>13),3266489909); h2=Math.imul(h2^(h2>>>16),2246822507)^Math.imul(h1^(h1>>>13),3266489909); return 4294967296*(31&(h2^h1>>>16))+(h1^h2>>>16)>>>0; }
  function deviceFP(){
    if(fpCache) return fpCache;
    var saved='';
    try{ saved=localStorage.getItem('canyou.device.id')||''; }catch(e){}
    if(/^[0-9a-f]{8,64}$/.test(saved)){ fpCache=saved; return fpCache; }
    try{
      var tz='na'; try{ tz=Intl.DateTimeFormat().resolvedOptions().timeZone; }catch(e){}
      var parts=[navigator.userAgent,screen.width+'x'+screen.height+'x'+screen.colorDepth,window.devicePixelRatio||1,
        navigator.language,tz,navigator.hardwareConcurrency||'na',navigator.deviceMemory||'na',
        navigator.maxTouchPoints||0,navigator.platform||'na',gpuInfo()];
      var s=parts.join('|');
      fpCache=cyrb53(s,1).toString(16).padStart(13,'0')+cyrb53(s,2).toString(16).padStart(13,'0');
      try{ localStorage.setItem('canyou.device.id',fpCache); }catch(e){}
    }catch(e){ fpCache=''; }
    return fpCache;
  }
  window.CANYOU_DID=deviceFP();
  // 设备名称：聊天昵称优先，没设过昵称时按 UA 推断平台；门禁授权时随 did 一并落库，便于后台名单复用
  window.canyouDeviceName=function(){
    var n='';
    try{ n=localStorage.getItem('canyou.chat.name')||''; }catch(e){}
    if(!n){
      var ua=navigator.userAgent;
      if(/Android/.test(ua)) n='Android 设备';
      else if(/iPhone|iPad|iPod/.test(ua)) n='iOS 设备';
      else if(/Windows/.test(ua)) n='Windows 电脑';
      else if(/Macintosh|Mac OS/.test(ua)) n='Mac';
      else if(/Linux/.test(ua)) n='Linux 电脑';
      else n='未知设备';
    }
    return n.slice(0,32);
  };
  // 不能放 header 的场合（页面跳转、下载链接）退回 URL 参数：给同源内部相对链接拼上 did；
  // 旧链接里残留的 did/id 必须先清掉再拼当前身份，否则失效旧值排在前面会永久卡住门禁识别
  window.canyouWithDid=function(href){
    if(!window.CANYOU_DID||typeof href!=='string'||href.charAt(0)!=='/'||href.charAt(1)==='/') return href;
    try{
      var u=new URL(href,location.href);
      u.searchParams.delete('did');
      u.searchParams.delete('id');
      u.searchParams.set('did',window.CANYOU_DID);
      return u.pathname+u.search+u.hash;
    }catch(e){
      if(href.indexOf('did=')!==-1) return href;
      return href+(href.indexOf('?')===-1?'?':'&')+'did='+window.CANYOU_DID;
    }
  };
  Array.prototype.forEach.call(document.querySelectorAll('a[href]'),function(a){
    var href=a.getAttribute('href');
    var patched=window.canyouWithDid(href);
    if(patched!==href) a.setAttribute('href',patched);
  });
  // UI 状态持久化：刷新后还原工作区视图，存不了时退回默认行为即可
  window.canyouUI=function(patch){
    try{
      var s={};
      try{ s=JSON.parse(localStorage.getItem('canyou.ui.state'))||{}; }catch(e){}
      if(typeof s!=='object'||Array.isArray(s))s={};
      for(var k in patch){ s[k]=patch[k]; }
      localStorage.setItem('canyou.ui.state',JSON.stringify(s));
    }catch(e){}
  };
  window.canyouReadUI=function(){
    try{var value=JSON.parse(localStorage.getItem('canyou.ui.state'));return value&&typeof value==='object'&&!Array.isArray(value) ? value : {};}catch(e){return {};}
  };
})();
</script>
{{end}}
{{define "chatStyles"}}
.tool-panel{min-width:0;min-height:0;padding:20px 16px 16px;display:flex;flex-direction:column;gap:12px;overflow:hidden}
.tool-panel[hidden],.tool-panel [hidden],#chatToggle[hidden]{display:none}
.tool-panel> :not(.panel-page){flex-shrink:0}
.chat-head,.chat-row{display:flex;align-items:center;gap:8px}
.chat-head{flex-wrap:nowrap;flex:none}.chat-head h2{font-size:17px;margin:0 auto 0 0;letter-spacing:-.3px}.chat-head .up-close{flex:none}.tool-panel .panel-close{min-width:44px;min-height:44px;align-items:center;justify-content:center}
.chat-toolbar{display:flex;align-items:center}.chat-toolbar .chat-status{flex:1}
.chat-head .tbtn{padding:5px 9px}.chat-head .tbtn[aria-expanded="true"]{border-color:var(--accent);color:var(--accent);background:var(--accent-soft)}
.chat-row .tbtn{flex:none}
.tool-panel input,.tool-panel textarea{font:inherit;color:var(--text);background:var(--bg);border:1px solid var(--border);border-radius:10px;padding:8px;min-width:0}
.tool-panel input:focus,.tool-panel textarea:focus{outline:none;border-color:var(--accent);box-shadow:0 0 0 3px var(--accent-soft)}
.tool-panel label{font-size:12px;color:var(--muted);flex:none}
.chat-status{font-size:12px;color:var(--muted);min-width:80px;overflow-wrap:anywhere;margin-right:auto}.chat-status::before{content:'';display:inline-block;width:6px;height:6px;border-radius:50%;background:currentColor;margin-right:6px}
.chat-status.bad{color:var(--bad)}.chat-status.ok{color:var(--ok)}
.chat-identity{font-size:11px;color:var(--muted);overflow-wrap:anywhere;font-variant-numeric:tabular-nums;flex:none;max-width:100%;padding-bottom:12px;border-bottom:1px solid var(--border)}
.panel-page{flex:1;min-height:0;display:flex;flex-direction:column;gap:12px;overflow:auto;overscroll-behavior:contain}
#chatNameBlock{display:flex;flex-direction:column;gap:6px;width:100%;max-width:300px}
#chatNameBlock .chat-row{width:100%}
#chatSettingsName #chatNameBlock{max-width:none}
.chat-name{flex:1;width:0;font-size:15px;padding:10px}
.chat-chat{overflow:hidden;position:relative}
.chat-log{flex:1;min-height:0;overflow:auto;overscroll-behavior:contain;padding:8px 2px 12px}
.chat-empty{font-size:12px;text-align:center;color:var(--muted);padding:18px 0}
.chat-msg-row{display:flex;align-items:flex-start;gap:8px;margin:0 0 10px}
.chat-msg-row.own{flex-direction:row-reverse}
.chat-msg-row .chat-message{margin:0;min-width:0;max-width:calc(100% - 44px)}
.chat-msg-row .chat-avatar{width:32px;height:32px;flex:none;font-size:14px}
.chat-msg-row .chat-avatar::after{display:none}
.chat-message{position:relative;padding:9px 12px;border:1px solid var(--border);border-radius:4px 12px 12px 12px;background:var(--card);overflow-wrap:anywhere;box-shadow:0 1px 3px rgba(20,25,40,.06)}
.chat-message.own{--own-bg:color-mix(in srgb,var(--accent) 5%,var(--card));--own-bd:color-mix(in srgb,var(--accent) 35%,var(--border));border-color:var(--own-bd);background:var(--own-bg);border-radius:12px 4px 12px 12px}
/* 靠头像一侧顶部伸出的圆角气泡尾巴：别人的朝左、自己的朝右 */
.chat-message:not(.own)::before,.chat-message.own::before{content:'';position:absolute;top:2px;width:10px;height:10px;transform:rotate(45deg)}
.chat-message:not(.own)::before{left:-6px;background:var(--card);border-left:1px solid var(--border);border-bottom:1px solid var(--border);border-bottom-left-radius:3px}
.chat-message.own::before{right:-6px;background:var(--own-bg);border-right:1px solid var(--own-bd);border-top:1px solid var(--own-bd);border-top-right-radius:3px}
.chat-meta{font-size:11px;color:var(--muted);margin-bottom:5px;white-space:pre-wrap}
.chat-body{font-size:14px;white-space:pre-wrap;line-height:1.7}
.chat-compose{position:relative;margin:0;flex-shrink:0}
.tool-panel .chat-compose textarea{display:block;width:100%;resize:none;min-height:46px;max-height:120px;font-size:14px;line-height:1.5;padding:12px;border-radius:10px}
.chat-compose-actions{display:flex;align-items:center;justify-content:space-between;gap:8px;margin-top:8px}.chat-compose-actions .btn{margin-left:auto}.chat-compose-hint{font-size:11px;color:var(--muted)}
.chat-compose .btn{min-width:68px;padding:8px 16px;white-space:nowrap;line-height:20px;font-size:13px}.chat-compose{padding-top:8px;border-top:1px solid var(--border)}
/* 未连接/断线时覆盖聊天区的手动连接页 */
.chat-offline{position:absolute;inset:0;z-index:6;display:flex;flex-direction:column;align-items:center;justify-content:center;gap:8px;background:var(--bg);padding:20px;text-align:center}
.chat-offline-icon{font-size:30px;line-height:1}
.chat-offline-title{font-size:14px;font-weight:650}
.chat-offline-text{font-size:12px;color:var(--muted);overflow-wrap:anywhere;max-width:320px}
.chat-offline .btn{margin-top:8px;width:auto;min-width:96px;height:38px;padding:8px 20px;font-size:14px;border-radius:10px}
/* 统一细滚动条：Firefox 用 scrollbar-*，Webkit 隐藏原生箭头按钮并画圆角滑块 */
.chat-log,#chatDeviceList,.panel-page{scrollbar-width:thin;scrollbar-color:var(--border) transparent}
.tool-panel ::-webkit-scrollbar{width:8px;height:8px}
.tool-panel ::-webkit-scrollbar-track{background:transparent}
.tool-panel ::-webkit-scrollbar-thumb{background:var(--border);border-radius:8px}
.tool-panel ::-webkit-scrollbar-thumb:hover{background:var(--muted)}
.tool-panel ::-webkit-scrollbar-button,.tool-panel ::-webkit-scrollbar-corner{display:none;width:0;height:0}
.chat-subcard{flex-shrink:0;border:1px solid var(--border);border-radius:12px;padding:12px;background:var(--bg);display:flex;flex-direction:column;gap:10px}
.chat-subcard:empty{display:none}
.chat-subcard .chat-row{flex-wrap:wrap}
.chat-card-head{display:flex;align-items:center;gap:10px}
.chat-card-icon{width:34px;height:34px;flex:none;border-radius:10px;background:var(--accent-soft);display:grid;place-items:center;font-size:17px}
.chat-card-title{font-size:13px;font-weight:650}
.chat-card-sub{font-size:11px;color:var(--muted);margin-top:1px}
.chat-count{margin-left:auto;flex:none;font-size:11px;font-weight:600;color:var(--accent);background:var(--accent-soft);border-radius:999px;padding:2px 9px}
#chatSettingsName #chatNameBlock{gap:4px}
#chatSettingsName label[for="chatName"]{display:none}
#chatLimit{width:78px;font-size:12px}
.chat-note{font-size:11px;color:var(--muted);margin:0}
.theme-options{display:flex;flex-wrap:wrap;gap:8px;min-width:0;border:0;margin:0;padding:0}
.tool-panel .theme-option{display:flex;align-items:center;gap:8px;flex:1 1 120px;min-height:44px;border:1px solid var(--border);border-radius:8px;padding:8px;color:var(--text);font-size:13px;cursor:pointer}
.theme-option:has(input:checked){border-color:var(--accent);background:var(--accent-soft)}
.theme-options:disabled .theme-option{opacity:.6;cursor:wait}
.tool-panel .theme-option input{width:18px;height:18px;flex:none;margin:0;padding:0;accent-color:var(--accent)}
#themeStatus.bad{color:var(--bad)}
#chatDID{overflow-wrap:anywhere;font-variant-numeric:tabular-nums}
.chat-title-no{font-weight:400;color:var(--muted);font-size:11px}
/* 头像底色选择器：8 个圆形色块，选中项带描边与勾号 */
.chat-swatches{display:flex;flex-wrap:wrap;gap:10px}
.chat-swatch{width:44px;height:44px;padding:0;border:4px solid var(--card);border-radius:50%;cursor:pointer;position:relative;transition:transform .1s}
.chat-swatch:hover:not(:disabled){transform:scale(1.08)}
.chat-swatch:disabled{opacity:.5;cursor:default}
.chat-swatch[aria-pressed="true"]{border-color:var(--accent);box-shadow:0 0 0 2px var(--accent-soft)}
.chat-swatch[aria-pressed="true"]::after{content:'✓';position:absolute;inset:0;display:grid;place-items:center;color:#fff;font-size:14px;font-weight:700;text-shadow:0 1px 2px rgba(0,0,0,.45)}
.panel-page ul{list-style:none;margin:0;padding:0}
#chatDeviceList{border-top:1px solid var(--border);padding-top:4px}
.chat-device{display:flex;align-items:center;gap:10px;padding:9px 2px;border-bottom:1px solid var(--border);overflow-wrap:anywhere}.chat-device:last-child{border-bottom:0}
.chat-avatar{position:relative;width:36px;height:36px;flex:none;border-radius:50%;display:grid;place-items:center;font-size:15px;font-weight:650;color:#fff}
.chat-avatar::after{content:'';position:absolute;right:-1px;bottom:-1px;width:10px;height:10px;border-radius:50%;background:var(--ok);border:2px solid var(--bg)}
.chat-device-info{min-width:0;flex:1}
.chat-device-name{font-size:13px;font-weight:600}
.chat-badge{display:inline-block;margin-left:6px;padding:1px 7px;border-radius:999px;background:var(--accent-soft);color:var(--accent);font-size:10px;font-weight:600;vertical-align:1px}
.chat-device-meta,.chat-system{font-size:11px;color:var(--muted);overflow-wrap:anywhere;font-variant-numeric:tabular-nums}
@keyframes chatPageIn{from{opacity:0;transform:translateY(6px)}}
.panel-page:not([hidden]){animation:chatPageIn .18s ease}
.chat-system{text-align:center;margin:8px 0;line-height:1.6}
@media(max-width:1099px){.tool-panel{padding:20px;max-width:100%}.chat-msg-row .chat-message{max-width:min(78%,640px)}.panel-close{display:inline-flex}}
@media(max-width:767px){.tool-panel{padding:12px;gap:8px}.tool-panel input,.tool-panel .chat-compose textarea{font-size:16px}.chat-identity{padding-bottom:8px}.chat-msg-row .chat-message{max-width:calc(100% - 44px)}.chat-head h2{font-size:16px}.chat-compose-hint{font-size:11px}}
body.keyboard-open .chat-identity,body.keyboard-open .chat-compose-hint{display:none}
body.keyboard-open .tool-panel{gap:6px;padding:8px 12px}
body.keyboard-open .chat-compose{display:grid;grid-template-columns:minmax(0,1fr) auto;align-items:end;gap:8px;padding-top:4px}
body.keyboard-open .chat-compose-actions{margin-top:0}
body.keyboard-open .tool-panel .chat-compose textarea{max-height:64px}
@media(max-height:500px){.chat-identity,.chat-compose-hint{display:none}.tool-panel{gap:6px;padding:8px 12px}.chat-compose{display:grid;grid-template-columns:minmax(0,1fr) auto;align-items:end;gap:8px;padding-top:4px}.tool-panel .chat-compose textarea{max-height:64px}.chat-compose-actions{margin-top:0}.chat-head h2{font-size:14px}}
{{end}}
{{define "workspacePanels"}}{{if .CanChat}}
<section class="card tool-panel" id="chatPanel" hidden aria-labelledby="chatTitle" data-ip="{{.ClientIP}}">
  <div class="chat-head"><h2 id="chatTitle">公共聊天室</h2><button class="up-close panel-close" id="chatClose" data-panel-close="chat" type="button" aria-label="关闭消息，返回文件"><svg class="icon" aria-hidden="true"><use href="#i-close"/></svg></button></div>
  <div class="chat-toolbar"><span class="chat-status" id="chatStatus" role="status">正在连接…</span></div>
  <div class="chat-identity" id="chatMyInfo" hidden></div>
  <div class="panel-page chat-chat" id="chatPageChat">
    <div class="chat-log" id="chatLog" role="log" aria-label="聊天记录" aria-live="polite"><div class="chat-empty">连接后加载已保留的公共聊天记录</div></div>
    <form class="chat-compose" id="chatForm"><textarea id="chatText" rows="2" placeholder="分享想法，或打个招呼…" aria-label="消息内容" disabled></textarea><div class="chat-compose-actions"><span class="chat-compose-hint" id="chatInputHint">Enter 发送 · Shift+Enter 换行</span><button class="btn" id="chatSend" type="submit" disabled>发送</button></div></form>
    <div class="chat-offline" id="chatOffline" hidden>
      <div class="chat-offline-icon"><svg class="icon" aria-hidden="true"><use href="#i-chat"/></svg></div>
      <div class="chat-offline-title">未连接聊天室</div>
      <div class="chat-offline-text" id="chatOfflineText">正在自动重连…</div>
      <button class="btn" id="chatManualConnect" type="button">重新连接</button>
    </div>
  </div>
</section>
<section class="card tool-panel" id="devicesPanel" hidden aria-labelledby="devicesTitle">
  <div class="chat-head"><h2 id="devicesTitle">在线设备</h2><button class="up-close panel-close" id="devicesClose" data-panel-close="devices" type="button" aria-label="关闭在线设备，返回文件"><svg class="icon" aria-hidden="true"><use href="#i-close"/></svg></button></div>
  <div class="chat-toolbar"><span class="chat-status" id="devicesStatus" role="status">正在连接设备服务…</span></div>
  <div class="panel-page" id="chatPageDevices">
    <div class="chat-subcard">
      <div class="chat-card-head"><span class="chat-card-icon"><svg class="icon" aria-hidden="true"><use href="#i-users"/></svg></span><div><div class="chat-card-title">在线设备</div></div><span class="chat-count" id="chatDevicesCount">0</span></div>
      <ul id="chatDeviceList" aria-label="在线设备列表"></ul>
      <p class="chat-note" id="chatDevicesNote">连接建立后显示实时在线设备</p>
    </div>
  </div>
</section>
<section class="card tool-panel" id="settingsPanel" hidden aria-labelledby="settingsTitle">
  <div class="chat-head"><h2 id="settingsTitle">设置</h2><button class="up-close panel-close" id="settingsClose" data-panel-close="settings" type="button" aria-label="关闭设置，返回文件"><svg class="icon" aria-hidden="true"><use href="#i-close"/></svg></button></div>
  <div class="chat-toolbar"><span class="chat-status" id="settingsStatus" role="status">正在连接，外观设置可独立使用</span></div>
  <div class="panel-page" id="chatPageSettings">
    <div class="chat-subcard">
      <div class="chat-card-title" id="themeTitle">外观</div>
      <fieldset class="theme-options" id="themeOptions" aria-labelledby="themeTitle" disabled>
        <label class="theme-option"><input type="radio" name="theme" id="themeDark" value="dark">深色模式</label>
        <label class="theme-option"><input type="radio" name="theme" id="themeLight" value="light">浅色模式</label>
        <label class="theme-option"><input type="radio" name="theme" id="themeSystem" value="system" checked>跟随系统</label>
      </fieldset>
      <p class="chat-note" id="themeStatus" role="status">正在读取主题…</p>
    </div>
    <div class="chat-subcard">
      <div class="chat-card-head"><span class="chat-card-icon"><svg class="icon" aria-hidden="true"><use href="#i-users"/></svg></span><div><div class="chat-card-title">名称<span class="chat-title-no"> · 设备编号：<span id="chatMyNo">—</span></span></div></div></div>
      <div id="chatSettingsName">
        <div id="chatNameBlock">
          <div class="chat-row"><label for="chatName">名称</label><input class="chat-name" id="chatName" autocomplete="off" placeholder="名称"><button class="tbtn" id="chatRename" type="button">保存名称</button></div>
          <div class="chat-note">设备标识：<span id="chatDID"></span> · 当前 IP：<span id="chatIP">{{.ClientIP}}</span></div>
        </div>
      </div>
    </div>
    <div class="chat-subcard">
      <div class="chat-card-head"><span class="chat-card-icon"><svg class="icon" aria-hidden="true"><use href="#i-settings"/></svg></span><div><div class="chat-card-title">头像颜色</div></div></div>
      <div class="chat-swatches" id="chatSwatches" role="group" aria-label="头像颜色"></div>
      <div class="chat-row"><p class="chat-note">选中色块后点击保存，其他设备同步更新</p><button class="tbtn" id="chatSaveColor" type="button" disabled>保存颜色</button></div>
    </div>
    <div class="chat-subcard">
      <div class="chat-card-head"><span class="chat-card-icon"><svg class="icon" aria-hidden="true"><use href="#i-chat"/></svg></span><div><div class="chat-card-title">聊天记录</div><div class="chat-card-sub">所有已连接设备可修改全局保留上限</div></div></div>
      <div class="chat-row"><label for="chatLimit">保留条数</label><input id="chatLimit" type="number" min="1" max="10000" step="1" value="200" disabled><button class="tbtn" id="chatSaveLimit" type="button" disabled>保存</button></div>
      <p class="chat-note">超出上限会删除最旧记录，且无法恢复；记录保存在主程序电脑上。</p>
    </div>
  </div>
</section>
{{end}}{{end}}
{{define "chatScript"}}{{if .CanChat}}
<script>
(function(){
  function el(id){ return document.getElementById(id); }
  var toggle=el('chatToggle'), panel=el('chatPanel');
  if (!toggle || !panel) return;
  var nameInput=el('chatName'), ip=el('chatIP'), status=el('chatStatus'), log=el('chatLog'), myInfo=el('chatMyInfo');
  var renameBtn=el('chatRename'), saveColor=el('chatSaveColor');
  var offlineEl=el('chatOffline'), offlineText=el('chatOfflineText'), manualBtn=el('chatManualConnect');
  var text=el('chatText'), sendBtn=el('chatSend'), limitInput=el('chatLimit'), saveLimit=el('chatSaveLimit');
  var socket=null, phase='idle', clientID='', limit=200, unread=0, sequence=0, connectTimer=null, available=true;
  // 断线自动重连：指数退避，最多自动重试 MAX_RECONNECT 次，之后转为手动连接页
  var reconnectTimer=null, reconnectAttempts=0, autoStopped=false, MAX_RECONNECT=5, offlineReason='';
  var records=[], notices=[], seen=new Set(), pending={};
  var settingsToggle=el('chatSettingsToggle'), devicesToggle=el('chatDevicesToggle'), deviceList=el('chatDeviceList');
  var chatPage=el('chatPageChat'), deviceCount=el('chatDevicesCount');
  var swatchWrap=el('chatSwatches'), myColor=-1, chosenColor=-1;
  var logPosition=0, followLatest=true;
  function messagesVisible(){return !document.hidden && !panel.hidden && !chatPage.hidden && !document.querySelector('dialog[open]');}
  function markRead(){if(messagesVisible()){unread=0;updateToggle();}}
  function restoreLog(){if(!panel.hidden && !chatPage.hidden) log.scrollTop=followLatest ? log.scrollHeight : logPosition;}
  log.addEventListener('scroll',function(){if(!panel.hidden && !chatPage.hidden){logPosition=log.scrollTop;followLatest=log.scrollHeight-log.scrollTop-log.clientHeight<60;}});
  document.addEventListener('canyou:beforelayout',function(){if(!panel.hidden){logPosition=log.scrollTop;followLatest=log.scrollHeight-log.scrollTop-log.clientHeight<60;}});
  document.addEventListener('canyou:layout',function(){restoreLog();markRead();});
  document.addEventListener('canyou:visibility',markRead);
  document.addEventListener('visibilitychange',markRead);
  // 平级视图由工作区统一管理；聊天连接和数据更新不改变导航。
  function touchInput(){return window.matchMedia('(pointer:coarse)').matches || window.innerWidth<768;}
  function inputHint(){el('chatInputHint').textContent=touchInput() ? '回车换行 · 点击发送' : 'Enter 发送 · Shift+Enter 换行';}
  window.addEventListener('resize',inputHint);inputHint();
  // 头像底色按设备序号（allowed_devices 主键）散列取色，同一设备颜色稳定；无序号时退回设备指纹。
  function avatarHue(id){ var h=0; for(var i=0;i<id.length;i++){ h=(h*31+id.charCodeAt(i))>>>0; } return h%360; }
  function colorKey(no, clientId){ return no ? String(no) : clientId; }
  // 预设头像底色（数量须与后端 chatAvatarColors=8 一致）；设备登记时随机分配、设置页可手动改
  var AVATAR_COLORS=['#4f6ef7','#0ea5a4','#22a06b','#f2884b','#8b5cf6','#e5484d','#ec4899','#eab308'];
  // 有预设色(0..7)用之；否则(未登记/老设备 color=-1)回退按编号散列取色
  function avatarColor(color, no, clientId){ return (color>=0 && color<AVATAR_COLORS.length) ? AVATAR_COLORS[color] : ('hsl('+avatarHue(colorKey(no,clientId))+' 62% 52%)'); }
  // 设置页底色选择器：渲染 8 个色块，选中态用 chosenColor，连接就绪且无待处理请求时才可点击
  function renderSwatches(){
    if(!swatchWrap) return;
    swatchWrap.textContent='';
    var enabled=phase==='ready' && !pending.color;
    AVATAR_COLORS.forEach(function(color,i){
      var btn=document.createElement('button');
      btn.type='button';btn.className='chat-swatch';btn.style.background=color;
      btn.setAttribute('aria-label','头像颜色 '+(i+1));
      btn.setAttribute('aria-pressed',String(i===chosenColor));
      btn.disabled=!enabled;
      btn.addEventListener('click',function(){
        if(i===chosenColor || phase!=='ready' || pending.color) return;
        chosenColor=i;renderSwatches();controls();
      });
      swatchWrap.appendChild(btn);
    });
  }
  function deviceTag(no){ return no ? ' · #'+no : ''; }
    // 统一时间戳格式：2026-11-17 9:49:58 AM
    function fmtStamp(ts){
      var d=new Date(ts), p=function(n){ return (n<10?'0':'')+n; };
      var h=d.getHours(), ap=h<12?'AM':'PM'; h=h%12; if(h===0)h=12;
      return d.getFullYear()+'-'+p(d.getMonth()+1)+'-'+p(d.getDate())+' '+h+':'+p(d.getMinutes())+':'+p(d.getSeconds())+' '+ap;
    }
  function renderDevices(devices){
    deviceList.textContent='';deviceCount.textContent=devices.length;
    var label='在线设备('+devices.length+')';el('chatDevicesLabel').textContent=label;devicesToggle.title=label;
    var note=el('chatDevicesNote');note.hidden=!!devices.length;
    devices.forEach(function(device){
      var row=document.createElement('li');row.className='chat-device';
      var avatar=document.createElement('div');avatar.className='chat-avatar';
      var initial=device.no ? String(device.no) : (Array.from(device.name||'?')[0]||'?');
      avatar.textContent=initial.toUpperCase();
      avatar.style.background=avatarColor(device.color,device.no,device.clientId);
      var box=document.createElement('div');box.className='chat-device-info';
      var name=document.createElement('div');name.className='chat-device-name';name.textContent=device.name;
      if(device.clientId===clientID){var badge=document.createElement('span');badge.className='chat-badge';badge.textContent='我';name.appendChild(badge);}
      var meta=document.createElement('div');meta.className='chat-device-meta';meta.textContent=device.ip+deviceTag(device.no)+(device.clientId ? ' · '+device.clientId : '');
      box.appendChild(name);box.appendChild(meta);row.appendChild(avatar);row.appendChild(box);deviceList.appendChild(row);
    });
  }
  function appendNotice(message,time){
    var nearBottom=log.scrollHeight-log.scrollTop-log.clientHeight<60;
    var placeholder=log.querySelector('.chat-empty');if(placeholder)placeholder.remove();
    var row=document.createElement('div');row.className='chat-system';
    row.textContent=fmtStamp(time || Date.now())+(message ? ' · '+message : '');log.appendChild(row);notices.push(row);
    // 系统提示不占用户消息条数，单独限制数量以避免长时间连接累积。
    while(notices.length>100)notices.shift().remove();
    if(nearBottom)log.scrollTop=log.scrollHeight;
  }
  function defaultName(){
    var ua=navigator.userAgent;
    if (/iPad/.test(ua) || (/Macintosh/.test(ua) && navigator.maxTouchPoints > 1)) return 'iPad';
    if (/iPhone|iPod/.test(ua)) return 'iPhone';
    if (/Android/.test(ua)) return /Mobile/.test(ua) ? 'Android 手机' : 'Android 平板';
    if (/Windows/.test(ua)) return 'Windows 电脑';
    if (/Macintosh|Mac OS/.test(ua)) return 'Mac';
    if (/Linux/.test(ua)) return 'Linux 电脑';
    return '未知设备';
  }
  // 设备唯一标识已由 identityScript 在页面加载时算好并挂到 window.CANYOU_DID，此处直接取用。
  var didEl=el('chatDID'); if(didEl) didEl.textContent=window.CANYOU_DID || '未知';
  var myNoEl=el('chatMyNo');
  try { nameInput.value=localStorage.getItem('canyou.chat.name') || defaultName(); }
  catch(e){ nameInput.value=defaultName(); }
  function rememberName(name){ try { localStorage.setItem('canyou.chat.name',name); } catch(e){} }
  function setStatus(message,kind,target){
    var node=target==='settings' ? el('settingsStatus') : status;
    node.textContent=message;node.className='chat-status '+(kind||'');
    if(!target){
      var devices=el('devicesStatus'), settings=el('settingsStatus');
      devices.textContent=phase==='ready' ? '设备列表实时更新' : '设备列表暂不可用 · '+message;
      devices.className='chat-status '+(kind||'');
      settings.textContent=phase==='ready' ? '设备设置已就绪' : '设备设置暂不可用，外观设置仍可使用';
      settings.className='chat-status '+(kind||'');
    }
  }
  function updateToggle(){
    document.querySelectorAll('[data-unread]').forEach(function(b){b.hidden=!unread;b.textContent=unread>99 ? '99+' : String(unread);b.setAttribute('aria-label',unread+' 条未读消息');});
  }
  function controls(){
    var ready=phase==='ready';
    // 未连接/断线时显示手动连接页（覆盖聊天区），ready/syncing 时隐藏
    var connected=phase==='ready'||phase==='syncing';
    if(!available || connected){ offlineEl.hidden=true; }
    else {
      offlineEl.hidden=false;
      if(phase==='connecting') offlineText.textContent='正在连接聊天室…';
      else if(autoStopped) offlineText.textContent=(offlineReason ? offlineReason+'：' : '')+'连接服务失败，请确认服务可用后手动连接';
      else offlineText.textContent='连接已断开，正在自动重连…';
      manualBtn.disabled=phase==='connecting';
    }
    // 在线设备入口始终占位，连接就绪前仅禁用，避免刷新或重连时顶栏跳动。
    devicesToggle.disabled=!ready;
    settingsToggle.hidden=false;
    nameInput.disabled=!ready || !!pending.rename;
    renameBtn.disabled=!ready || !!pending.rename;
    text.disabled=!ready || !!pending.message;
    sendBtn.disabled=!ready || !!pending.message || !text.value.trim();
    sendBtn.textContent=pending.message ? '发送中' : '发送';
    limitInput.disabled=!ready || !!pending.retention;
    saveLimit.disabled=!ready || !!pending.retention;
    // 底色色块：连接就绪且无待处理改色请求时可点；保存按钮仅在选择与已保存值不同时可用
    var swatchEnabled=ready && !pending.color;
    swatchWrap.querySelectorAll('.chat-swatch').forEach(function(b){b.disabled=!swatchEnabled;});
    saveColor.disabled=!swatchEnabled || chosenColor<0 || chosenColor===myColor;
    saveColor.textContent=pending.color ? '保存中' : '保存颜色';
  }
  function resetPending(){ pending={}; controls(); }
  function clearReconnect(){ if(reconnectTimer){ clearTimeout(reconnectTimer); reconnectTimer=null; } }
  function dropSocket(){ var old=socket; socket=null; clearTimeout(connectTimer); phase='idle'; if(old){ try{ old.close(); }catch(e){} } }
  // 手动点击“连接/重新连接”：清零退避计数后重新尝试
  function manualConnect(){ reconnectAttempts=0; autoStopped=false; offlineReason=''; clearReconnect(); connect(true); }
  // 指数退避自动重连，超过上限则放弃自动、转手动连接页
  function scheduleReconnect(){
    clearReconnect();
    reconnectAttempts++;
    if(reconnectAttempts>MAX_RECONNECT){ autoStopped=true; setStatus('连接服务失败，请确认服务可用后手动连接','bad'); controls(); return; }
    var delay=Math.min(2000*Math.pow(2,reconnectAttempts-1),20000);
    setStatus('连接已断开，'+Math.round(delay/1000)+' 秒后自动重连（第 '+reconnectAttempts+'/'+MAX_RECONNECT+' 次）…','bad');
    reconnectTimer=setTimeout(function(){ reconnectTimer=null; if(socket || !available) return; connect(); }, delay);
    controls();
  }
  // 断线统一入口：仍可用且未放弃自动重连则退避重试，否则停在手动连接页
  function handleDisconnect(){
    renderDevices([]);
    if(!available || autoStopped){ controls(); return; }
    scheduleReconnect();
  }
  function prune(oldest){
    while(records.length && ((oldest && records[0].id<oldest) || records.length>limit)){
      var row=records.shift(); seen.delete(row.id); row.node.remove();
    }
  }
  function appendMessages(messages,historical){
    var nearBottom=log.scrollHeight-log.scrollTop-log.clientHeight<60;
    var fragment=document.createDocumentFragment(), added=0;
    messages.forEach(function(m){
      if(seen.has(m.id)) return;
      seen.add(m.id); added++;
      var own=m.clientId===clientID;
      var row=document.createElement('div'); row.className='chat-msg-row'+(own ? ' own' : '');
      var avatar=document.createElement('div'); avatar.className='chat-avatar';
      var initial=m.no ? String(m.no) : (Array.from(m.name||'?')[0]||'?');
      avatar.textContent=initial.toUpperCase();
      avatar.dataset.cid=m.clientId; avatar.dataset.no=m.no;
      avatar.style.background=avatarColor(m.color,m.no,m.clientId);
      var msg=document.createElement('div'); msg.className='chat-message'+(own ? ' own' : '');
      var meta=document.createElement('div'); meta.className='chat-meta';
      meta.textContent=own ? fmtStamp(m.time) : (m.name+'（'+m.ip+'）\n'+fmtStamp(m.time));
      var body=document.createElement('div'); body.className='chat-body'; body.textContent=m.text;
      msg.appendChild(meta); msg.appendChild(body);
      row.appendChild(avatar); row.appendChild(msg);
      fragment.appendChild(row); records.push({id:m.id,node:row});
    });
    var placeholder=log.querySelector('.chat-empty'); if(placeholder && added) placeholder.remove();
    log.appendChild(fragment); prune(0);
    if(historical){followLatest=true;restoreLog();}
    else if(followLatest && nearBottom){restoreLog();}
    if(!historical && !messagesVisible()){unread+=added;updateToggle();}
  }
  // 头像颜色更新后，刷新聊天区已渲染历史消息的头像底色（按 clientId 匹配）
  function recolorMessages(clientId,color){
    var nodes=log.querySelectorAll('.chat-avatar');
    for(var i=0;i<nodes.length;i++){ var a=nodes[i];
      if(a.dataset.cid===clientId) a.style.background=avatarColor(color,Number(a.dataset.no)||0,clientId);
    }
  }
  function validName(){
    var name=nameInput.value.trim();
    if(!name || Array.from(name).length>32 || /[\u0000-\u001f\u007f-\u009f]/.test(name)){
      setStatus('设备名称须为 1–32 个字符，不能含控制字符','bad','settings'); return '';
    }
    return name;
  }
  function send(type,fields){
    if(!socket || socket.readyState!==WebSocket.OPEN || phase!=='ready') return false;
    var requestId=Date.now().toString(36)+'-'+(++sequence)+'-'+Math.random().toString(36).slice(2);
    var data=Object.assign({type:type,requestId:requestId},fields);
    try{socket.send(JSON.stringify(data));pending[type]={id:requestId};controls();return true;}
    catch(e){delete pending[type];controls();setStatus('发送失败，输入已保留，请重新连接','bad',type==='message'?'chat':'settings');return false;}
  }
  function connect(manual){
    if(socket || !available) return;
    if(manual){ reconnectAttempts=0; autoStopped=false; offlineReason=''; }
    clearReconnect();
    sequence++;
    var ws;
    var fp=window.CANYOU_DID||'';
    // 连接不再传 name：设备名由后台按 allowed_devices 登记下发，welcome 事件里采用并回显
    try{ws=new WebSocket((location.protocol==='https:' ? 'wss://' : 'ws://')+location.host+'/ws/chat?'+(fp ? 'id='+fp : ''));}
    catch(e){ setStatus('无法建立消息连接，请检查浏览器或网络','bad'); handleDisconnect(); return; }
    socket=ws;phase='connecting';resetPending();setStatus('正在连接…');controls();
    connectTimer=setTimeout(function(){ if(socket===ws && phase!=='ready'){ setStatus('连接超时，正在重试…','bad'); try{ws.close();}catch(e){} } },20000);
    ws.onopen=function(){if(socket===ws){phase='syncing';setStatus('正在加载历史…');controls();}};
    ws.onmessage=function(event){
      if(socket!==ws) return;
      var data; try{data=JSON.parse(event.data);}catch(e){setStatus('服务器消息格式错误','bad');return;}
      switch(data.type){
        case 'welcome':
          clientID=data.clientId; nameInput.value=data.name; rememberName(data.name); ip.textContent=data.ip;
          myInfo.textContent=data.name+'（'+data.ip+'）'; myInfo.hidden=false;
          limit=data.limit;limitInput.value=limit;records=[];notices=[];seen.clear();log.textContent='';markRead();renderDevices(data.devices || []);
          myColor=data.color;chosenColor=data.color;renderSwatches();
          if(myNoEl) myNoEl.textContent=data.no ? '#'+data.no : '未登记';break;
        case 'history':appendMessages(data.messages || [],true);break;
        case 'ready':
          clearTimeout(connectTimer);clearReconnect();reconnectAttempts=0;autoStopped=false;offlineReason='';phase='ready';setStatus('已连接 · 公共聊天室','ok');
          if(!records.length){var empty=document.createElement('div');empty.className='chat-empty';empty.textContent='暂无聊天记录，发送第一条消息吧';log.appendChild(empty);}
          controls();restoreLog();markRead();break;
        case 'message':
          if(data.limit!==limit){limit=data.limit;limitInput.value=limit;}appendMessages([data.message],false);
          if(data.message.clientId===clientID && pending.message && pending.message.id===data.requestId){text.value='';autosize();delete pending.message;controls();setStatus('消息已保存并发送','ok','chat');if(messagesVisible() && !touchInput())text.focus({preventScroll:true});}break;
        case 'presence':
          renderDevices(data.devices || []);
          if(data.action==='joined' || data.action==='left'){
            // 提示格式：时间 · #编号 · 名称 · IP · 动作（时间由 appendNotice 置于最前）
            var parts=[];
            if(data.no) parts.push('#'+data.no);
            parts.push(data.name+(data.clientId===clientID ? '（我）' : ''));
            parts.push(data.ip);
            parts.push(data.action==='joined' ? '已连接' : '已断开');
            appendNotice(parts.join(' · '),data.time);
          }
          break;
        case 'renamed':
          if(data.devices)renderDevices(data.devices);
          if(data.clientId===clientID){nameInput.value=data.name;rememberName(data.name);myInfo.textContent=data.name+'（'+(ip.textContent||'')+'）';delete pending.rename;controls();setStatus('设备名称已保存','ok','settings');}break;
        case 'recolored':
          if(data.devices)renderDevices(data.devices);
          recolorMessages(data.clientId,data.color);
          if(data.clientId===clientID){myColor=data.color;chosenColor=data.color;renderSwatches();delete pending.color;controls();setStatus('头像颜色已更新','ok','settings');}break;
        case 'settings':
          limit=data.limit;limitInput.value=limit;prune(data.oldestId);
          if(pending.retention && pending.retention.id===data.requestId) delete pending.retention;
          controls();setStatus('全局保留上限已更新：'+limit+' 条','ok','settings');break;
        case 'error':
          var failedType=Object.keys(pending).find(function(key){return pending[key].id===data.requestId;});
          var failedColor=failedType==='color';
          Object.keys(pending).forEach(function(key){if(pending[key].id===data.requestId)delete pending[key];});
          if(failedColor){chosenColor=myColor;renderSwatches();}
          controls();setStatus(data.error || '操作失败','bad',failedType&&failedType!=='message'?'settings':'chat');break;
        case 'disabled':
          offlineReason='主程序已关闭消息'; dropSocket(); resetPending(); handleDisconnect(); break;
      }
    };
    ws.onerror=function(){if(socket===ws)setStatus('消息连接失败，正在重试…','bad');};
    ws.onclose=function(event){
      if(socket!==ws) return;
      var wasReady=phase==='ready';socket=null;phase='idle';clearTimeout(connectTimer);resetPending();
      if(event.code===1000) return; // pagehide 主动关闭，不触发重连
      if(event.code===4001){ offlineReason='主程序已关闭消息'; }
      if(wasReady)appendNotice('你已断开聊天室连接');
      handleDisconnect();
    };
  }
  manualBtn.addEventListener('click',manualConnect);
  saveColor.addEventListener('click',function(){
    if(phase!=='ready' || pending.color || chosenColor<0 || chosenColor===myColor) return;
    send('color',{color:chosenColor});
  });
  // 离开页面只静默关连接、不写导航状态；重连由加载末尾自动接入完成
  window.addEventListener('pagehide',function(){ clearReconnect(); var old=socket; socket=null; clearTimeout(connectTimer); if(old){ try{old.close(1000,'page leaving');}catch(e){} } });
  renameBtn.addEventListener('click',function(){var name=validName();if(name)send('rename',{name:name});});
  // 输入框随内容自动增高（46–120px），替代手动拖拽调整大小。
  function autosize(){ text.style.height='auto'; text.style.height=Math.min(window.innerHeight<500 ? 64 : 120,Math.max(46,text.scrollHeight))+'px'; }
  text.addEventListener('input',function(){autosize();controls();});
  var composing=false;
  text.addEventListener('compositionstart',function(){composing=true;});
  text.addEventListener('compositionend',function(){composing=false;});
  function sendMessage(){
    if(pending.message) return;
    var body=text.value.trim();
    if(!body || Array.from(body).length>2000){setStatus('消息须为 1–2000 个字符','bad','chat');return;}
    send('message',{text:body});
  }
  el('chatForm').addEventListener('submit',function(e){e.preventDefault();sendMessage();});
  text.addEventListener('keydown',function(e){if(e.key==='Enter' && !touchInput() && !e.shiftKey && !e.isComposing && !composing && e.keyCode!==229){e.preventDefault();sendMessage();}});
  saveLimit.addEventListener('click',function(){
    var value=limitInput.value;
    if(!/^\d+$/.test(value) || Number(value)<1 || Number(value)>10000){setStatus('保留条数须为 1–10000 的整数','bad','settings');return;}
    var next=Number(value);
    if(next<limit && !window.confirm('减少全局上限将立即删除超额旧记录，且无法恢复。确定保留最新 '+next+' 条吗？')){limitInput.value=limit;return;}
    send('retention',{limit:next});
  });
  controls();
  // 权威设备名来自门禁 allowed_devices：能打开本页说明已过门禁或未设密码，
  // 先用 /gate/check 拿登记名作为聊天昵称，再自动接入；工作区独立恢复导航状态；
  // check 失败（如网络异常）回退到已存昵称或平台默认名，一律不阻断自动接入
  function boot(){ connect(); }
  window.canyouDeviceReady
    .then(function(d){ if(d && d.allowed && d.name){ nameInput.value=d.name; rememberName(d.name); } boot(); })
    .catch(boot);
})();
</script>
{{end}}{{end}}
`
