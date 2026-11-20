
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
    }).then(function(r){return r.json().then(function(data){if(!r.ok||(!data.ok&&!(path==='/gate/check'&&data.allowed===false)))throw new Error(data.message||'请求失败');return data;});})
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
