
(function(){
  var theme=window.canyouTheme, group=document.getElementById('themeOptions');
  if(group)group.addEventListener('change',function(e){if(e.target.name==='theme')theme.save(e.target.value);});
  theme.render();
  // 与聊天共享设备信息请求；主题恢复不依赖聊天模板或 WebSocket。
  var did=window.CANYOU_DID||'';
  window.canyouDeviceReady=theme.request('/gate/check',{did:did})
    .then(function(data){
      if(data.allowed)return data;
      if(data.requiresPassword===false)return theme.request('/gate/unlock',{did:did,name:window.canyouDeviceName?canyouDeviceName():''});
      // 旧缓存曾放行当前页面时，check 已清除授权，重新导航即可显示密码页。
      if(data.requiresPassword===true)location.replace(window.canyouWithDid(location.pathname+location.search+location.hash));
      throw new Error('设备尚未授权');
    })
    .then(function(data){if(!data.ok||!data.allowed)throw new Error('设备尚未授权');theme.accept(data.theme);return data;})
    .catch(function(){return null;})
    .then(function(data){theme.ready(!!data);return data;});
})();
