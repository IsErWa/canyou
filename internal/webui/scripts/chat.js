
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
