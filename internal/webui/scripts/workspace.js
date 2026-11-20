
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
