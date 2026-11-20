
(function(){
  var did=window.CANYOU_DID||'';
  var next=JSON.parse(document.getElementById('gateInit').textContent).next;
  var note=document.getElementById('gateNote'), form=document.getElementById('gateForm');
  var pass=document.getElementById('gatePass'), btn=document.getElementById('gateBtn'), err=document.getElementById('gateError');
  function showError(message){ showForm(message); btn.disabled=false; }
  function unlock(password){return window.canyouTheme.request('/gate/unlock',{did:did,password:password,name:window.canyouDeviceName?canyouDeviceName():''});}
  function accept(data){if(!data.ok||!data.allowed)throw new Error('验证失败，请重试');window.canyouTheme.accept(data.theme);into();}
  function showForm(tip){ if(form.hidden){ note.hidden=true; form.hidden=false; pass.focus(); } err.textContent=tip; err.hidden=!tip; }
  // 带着当前 did 跳回原目标：canyouWithDid 会先清掉旧链接残留的 did，避免失效身份卡在参数首位死循环
  function into(){ location.replace(window.canyouWithDid(next)); }
  if(!did){ showForm('浏览器安全存储不可用，验证可能无法保持，仍可尝试密码'); return; }
  window.canyouTheme.request('/gate/check',{did:did})
    .then(function(data){
      if(data.allowed){accept(data);return;}
      if(data.requiresPassword===false)return unlock('').then(accept);
      showForm('');
    })
    .catch(function(error){showError(error.message||'验证失败，请重试');});
  form.addEventListener('submit',function(e){
    e.preventDefault();
    btn.disabled=true; err.hidden=true;
    unlock(pass.value).then(accept)
      .catch(function(error){showError(error.message||'网络错误，请重试');});
  });
})();
