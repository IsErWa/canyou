
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
