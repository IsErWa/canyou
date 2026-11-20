
(function(){
  var input = document.getElementById('fileInput');
  var dz = document.getElementById('dz');
  var chips = document.getElementById('chips');
  var pager = document.getElementById('pager');
  var submit = document.getElementById('submit');
  var hint = document.getElementById('hint');
  var form = document.getElementById('uploadPanel');
  var resultBox = document.getElementById('uploadResult');
  if (!input) return;

  var PAGE_SIZE = 5; // 每页固定条数，避免文件过多时窗口不断向上撑高
  var page = 0;
  var selected = []; // 已选文件；支持逐个移除，提交前同步回 input.files
  var uploading = false;
  function setUploading(value){
    uploading = value; input.disabled = value;
    dz.setAttribute('aria-disabled', String(value)); form.setAttribute('aria-busy', String(value));
    document.getElementById('uploadLabel').textContent = value ? '上传中…' : '上传文件';
    submit.textContent = value ? '上传中…' : '上传文件';
    render();
  }

  function fmt(b){
    if (b < 1024) return b + ' B';
    var u = ['KB','MB','GB','TB','PB'], i = -1;
    do { b /= 1024; i++; } while (b >= 1024 && i < u.length - 1);
    return b.toFixed(1) + ' ' + u[i];
  }
  function totalPages(){ return Math.max(1, Math.ceil(selected.length / PAGE_SIZE)); }
  function syncInput(){ // 把 selected 重建到 input.files，保证原生表单提交只携带未移除的文件
    var dt = new DataTransfer();
    selected.forEach(function(f){ dt.items.add(f); });
    input.files = dt.files;
  }
  function removeAt(index){
    if (uploading) return;
    selected.splice(index, 1);
    hideResult();
    syncInput();
    render();
  }
  function addFiles(fileList){
    if (uploading) return;
    hideResult();
    var before = selected.length;
    Array.prototype.forEach.call(fileList, function(f){
      var dup = selected.some(function(s){ return s.name === f.name && s.size === f.size; });
      if (!dup) selected.push(f);
    });
    if (selected.length !== before) page = totalPages() - 1; // 有新文件则跳到最后一页
    syncInput();
    render();
  }
  function renderPager(){
    var tp = totalPages();
    if (selected.length <= PAGE_SIZE){ pager.hidden = true; pager.innerHTML = ''; return; }
    pager.hidden = false;
    pager.innerHTML = '';
    var prev = document.createElement('button'); prev.type = 'button'; prev.className = 'tbtn'; prev.textContent = '‹ 上一页'; prev.disabled = (page === 0);
    prev.addEventListener('click', function(){ if (page > 0){ page--; render(); } });
    var info = document.createElement('span'); info.className = 'pager-info'; info.textContent = (page + 1) + ' / ' + tp;
    var next = document.createElement('button'); next.type = 'button'; next.className = 'tbtn'; next.textContent = '下一页 ›'; next.disabled = (page >= tp - 1);
    next.addEventListener('click', function(){ if (page < tp - 1){ page++; render(); } });
    pager.appendChild(prev); pager.appendChild(info); pager.appendChild(next);
  }
  function render(){
    chips.innerHTML = '';
    var tp = totalPages();
    if (page > tp - 1) page = tp - 1;
    if (page < 0) page = 0;
    var start = page * PAGE_SIZE;
    var end = Math.min(start + PAGE_SIZE, selected.length);
    for (var i = start; i < end; i++){
      var f = selected[i];
      var chip = document.createElement('span'); chip.className = 'chip';
      var nm = document.createElement('span'); nm.className = 'nm'; nm.textContent = f.name; nm.title = f.name;
      var sz = document.createElement('span'); sz.className = 'sz'; sz.textContent = fmt(f.size);
      var rm = document.createElement('button'); rm.type = 'button'; rm.className = 'chip-x'; rm.appendChild(window.canyouIcon('close')); rm.setAttribute('aria-label', '移除 ' + f.name);
      (function(index){ rm.addEventListener('click', function(){ removeAt(index); }); })(i);
      rm.disabled = uploading;
      chip.appendChild(nm); chip.appendChild(sz); chip.appendChild(rm);
      chips.appendChild(chip);
    }
    renderPager();
    var n = selected.length;
    submit.disabled = uploading || (n === 0);
    hint.textContent = uploading ? '正在上传，请勿关闭网页' : (n ? ('已选择 ' + n + ' 个文件') : '尚未选择文件');
  }
  input.addEventListener('change', function(){ addFiles(input.files); });
  ['dragenter','dragover'].forEach(function(ev){
    dz.addEventListener(ev, function(e){ e.preventDefault(); dz.classList.add('over'); });
  });
  ['dragleave','dragend'].forEach(function(ev){
    dz.addEventListener(ev, function(){ dz.classList.remove('over'); });
  });
  dz.addEventListener('drop', function(e){
    e.preventDefault(); dz.classList.remove('over');
    if (e.dataTransfer && e.dataTransfer.files.length) addFiles(e.dataTransfer.files);
  });

  function clearSelected(){
    selected = [];
    page = 0;
    syncInput();
    render();
  }
  function hideResult(){
    if (!resultBox) return;
    resultBox.hidden = true;
    resultBox.textContent = '';
    resultBox.className = 'up-result';
  }
  function showResult(data){
    if (!resultBox) return;
    resultBox.textContent = '';
    var failed = data.failed || [];
    var partial = data.ok && failed.length > 0;
    resultBox.className = 'up-result ' + (partial ? 'partial' : (data.ok ? 'ok' : 'bad'));
    resultBox.hidden = false;
    var head = document.createElement('div');
    head.textContent = data.message || (partial ? '部分文件上传失败' : (data.ok ? '上传成功' : '上传失败'));
    resultBox.appendChild(head);
    if (failed.length){
      var ul = document.createElement('ul');
      failed.forEach(function(item){ var li = document.createElement('li'); li.textContent = item; ul.appendChild(li); });
      resultBox.appendChild(ul);
    }
  }
  // 就地上传：fetch 提交后在悬浮窗内展示结果，全部成功则清空列表，不再跳转独立结果页
  if (form) form.addEventListener('submit', function(e){
    e.preventDefault();
    if (uploading || !selected.length) return;
    hideResult();
    var payload = new FormData(form);
    setUploading(true);
    fetch('/upload', { method: 'post', body: payload, headers: {'X-Device-ID': window.CANYOU_DID || ''} })
      .then(function(resp){ return resp.json().catch(function(){ return {ok:false, message:'服务器响应异常'}; }); })
      .then(function(data){
        showResult(data);
        if (data.ok && !(data.failed && data.failed.length)) clearSelected();
      })
      .catch(function(){ showResult({ok:false, message:'上传失败：网络错误'}); })
      .then(function(){ setUploading(false); });
  });
})();
