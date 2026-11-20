
(function(){
  var zipBtn = document.getElementById('zipBtn');
  var list = document.getElementById('fileList');
  if (!zipBtn || !list) return;
  var card = document.querySelector('.card.files');
  var pathTitle = document.getElementById('pathTitle');
  var selHint = document.getElementById('selHint');
  var selAll = document.getElementById('selAll');
  var selNone = document.getElementById('selNone');
  var current = '/', requestedPath = '/', listRequest = 0;
  var parentBtn = document.getElementById('parentBtn');
  var lastCount = 0;
  var media = []; // 当前目录里可进浮层预览的媒体条目（{name,path,link,kind}），左右翻页用

  // 简化版 path.Clean("/"+p)：恒有首斜杠，消掉 . 与 .. 段
  function pathClean(p){
    var stack = [];
    String(p || '').split('/').forEach(function(seg){
      if (!seg || seg === '.') return;
      if (seg === '..') { stack.pop(); return; }
      stack.push(seg);
    });
    return stack.length ? '/' + stack.join('/') : '/';
  }

  // 列表动态重渲染，勾选项每次从容器内实时取
  function boxes(){ return Array.prototype.slice.call(list.querySelectorAll('.pick')); }
  function checkedItems(){
    return boxes().filter(function(b){ return b.checked; })
      .map(function(b){ return {path: b.value, dir: b.getAttribute('data-dir') === '1'}; });
  }
  function update(){
    var n = checkedItems().length;
    zipBtn.disabled = (n === 0);
    selAll.disabled = lastCount === 0 || n === lastCount;
    selNone.disabled = n === 0;
    boxes().forEach(function(b){ b.closest('.row').classList.toggle('selected', b.checked); });
    if (selHint) selHint.textContent = n ? ('已选 ' + n + ' 项') : ('共 ' + lastCount + ' 项');
  }

  // 统一的流式打包提交：base 为当前查看目录（zip 命名根），paths 为选中项完整路径；
  // POST 表单带不了 header，身份改拼在 action 的 URL 参数上
  function submitZip(paths){
    var base = card ? (card.getAttribute('data-base') || '/') : '/';
    var form = document.createElement('form');
    form.method = 'post'; form.action = window.canyouWithDid('/zip'); form.style.display = 'none';
    var b = document.createElement('input');
    b.type = 'hidden'; b.name = 'base'; b.value = base;
    form.appendChild(b);
    paths.forEach(function(p){
      var input = document.createElement('input');
      input.type = 'hidden'; input.name = 'paths'; input.value = p;
      form.appendChild(input);
    });
    document.body.appendChild(form);
    form.submit();
  }

  // 文件名是不可信数据：一律 createElement + textContent 渲染，禁止 innerHTML 拼接
  function renderBreadcrumbs(){
    pathTitle.textContent = '';
    var parts = current.split('/').filter(Boolean), target = '';
    function crumb(label, p, active){
      var b = document.createElement('button'); b.type = 'button'; b.textContent = label; b.title = p;
      if (active) b.setAttribute('aria-current', 'page');
      b.addEventListener('click', function(){ if(p !== current) navigate(p); });
      pathTitle.appendChild(b);
    }
    crumb('根目录', '/', !parts.length);
    parts.forEach(function(part, i){
      var sep = document.createElement('span'); sep.textContent = '/'; sep.setAttribute('aria-hidden', 'true'); pathTitle.appendChild(sep);
      target += '/' + part; crumb(part, target, i === parts.length - 1);
    });
    parentBtn.disabled = current === '/';
    pathTitle.scrollLeft = pathTitle.scrollWidth;
  }
  parentBtn.addEventListener('click', function(){ if(current !== '/') navigate(pathClean(current + '/..')); });
  function makeRow(item){
    var row = document.createElement('div'); row.className = 'row';
    var ck = document.createElement('label'); ck.className = 'ck';
    var box = document.createElement('input'); box.type = 'checkbox'; box.className = 'pick';
    box.value = item.path; box.setAttribute('data-dir', item.isDir ? '1' : '0');
    box.setAttribute('aria-label', '选择 ' + item.name);
    ck.appendChild(box);
    var name = document.createElement('span'); name.className = 'name';
    var fi = document.createElement('span'); fi.className = 'fi' + (item.isDir ? ' folder' : '');
    fi.appendChild(window.canyouIcon(item.isDir ? 'folder' : 'file'));
    var a = document.createElement('a'); a.href = item.link; a.title = item.name;
    var fileLabel = document.createElement('span'); fileLabel.className = 'file-label'; fileLabel.textContent = item.name; a.appendChild(fileLabel);
    if (item.isDir) a.setAttribute('data-directory', item.path);
    if (!item.isDir){
      // identityScript 只在加载时补链一次，动态生成的文件链接要自己带 did
      a.href = window.canyouWithDid(item.link);
      var kind = kindOf(item.name);
      if (kind === 'image' || kind === 'video' || kind === 'audio') { // 媒体：点击弹浮层预览而非下载，下载仍走行尾按钮
        a.className = 'preview';
        a.setAttribute('data-path', item.path);
        a.title = (kind === 'image' ? '预览 ' : '播放 ') + item.name;
      } else if (kind === 'blank') { // PDF/HTML/文本代码：新标签页，浏览器原生渲染
        a.className = 'blank';
        a.target = '_blank';
        a.rel = 'noopener';
        a.title = '新标签页打开 ' + item.name;
      } else {
        a.setAttribute('download', item.name);
      }
    }
    name.appendChild(fi); name.appendChild(a);
    var size = document.createElement('span'); size.className = 'size'; size.textContent = item.size || '';
    var date = document.createElement('span'); date.className = 'date'; date.textContent = item.modTime || '';
    var dl = document.createElement('a'); dl.appendChild(window.canyouIcon('download'));
    dl.setAttribute('aria-label', (item.isDir ? '打包下载 ' : '下载 ') + item.name);
    if (item.isDir) {
      dl.className = 'dl dlpick'; dl.href = '#';
      dl.setAttribute('data-path', item.path);
      dl.title = '打包下载 ' + item.name;
    } else {
      dl.className = 'dl';
      dl.href = window.canyouWithDid(item.link);
      dl.setAttribute('download', item.name);
      dl.title = '下载 ' + item.name;
    }
    var meta = document.createElement('span'); meta.className = 'file-meta'; meta.appendChild(size); meta.appendChild(date);
    row.appendChild(ck); row.appendChild(name); row.appendChild(meta); row.appendChild(dl);
    return row;
  }

  // ===== 媒体预览浮层：innerHTML 全为静态骨架，文件名一律 textContent 写入 =====
  // 按扩展名分流：image/video/audio 进浮层，pdf/html/文本代码开新标签，其余（doc/zip 等）仍下载
  function kindOf(name){
    if (/\.(png|jpe?g|gif|webp|bmp|ico|avif|svg)$/i.test(name)) return 'image';
    if (/\.(mp4|webm)$/i.test(name)) return 'video';
    if (/\.(mp3|wav|ogg|oga|flac|m4a|aac)$/i.test(name)) return 'audio';
    if (/\.(pdf|html?|txt|log|md|csv|json|xml|ya?ml|ini|conf|cnf|toml|properties|sql|sh|bat|ps1|py|jsx?|tsx?|go|cpp|hpp|c|h|java|rs|rb|php|css|vue|swift|kt|lua)$/i.test(name)) return 'blank';
    return '';
  }
  var lb = null, lbName = null, lbPos = null, lbIndex = -1;
  function buildLightbox(){
    lb = document.createElement('dialog'); lb.className = 'lightbox'; lb.hidden = true;
    lb.setAttribute('aria-label', '媒体预览');
    lb.innerHTML = '<div class="lb-head"><span class="lb-name"></span><span class="lb-pos"></span><button class="lb-x" type="button" aria-label="关闭预览"></button></div>' +
      '<div class="lb-body"><button class="lb-nav lb-prev" type="button" aria-label="上一项"></button><div class="lb-media"></div><button class="lb-nav lb-next" type="button" aria-label="下一项"></button></div>';
    lb.querySelector('.lb-x').appendChild(window.canyouIcon('close'));
    lb.querySelector('.lb-prev').appendChild(window.canyouIcon('prev'));
    lb.querySelector('.lb-next').appendChild(window.canyouIcon('next'));
    document.body.appendChild(lb);
    window.canyouDialog(lb);
    lb.addEventListener('cancel', function(e){ e.preventDefault(); closePreview(); });
    lb.addEventListener('close', function(){ if(lb.open) return; lb.hidden = true; releaseMedia(); lbIndex = -1; });
    lbName = lb.querySelector('.lb-name'); lbPos = lb.querySelector('.lb-pos');
    lb.addEventListener('click', function(e){
      var t = e.target;
      if (t === lb) { var r=lb.getBoundingClientRect(); if(e.clientX<r.left || e.clientX>r.right || e.clientY<r.top || e.clientY>r.bottom) closePreview(); return; } // 点媒体外围关闭
      var button = t.closest('button');
      if (!button) return;
      if (button.classList.contains('lb-x')) closePreview();
      if (button.classList.contains('lb-prev')) showPreview(lbIndex - 1);
      if (button.classList.contains('lb-next')) showPreview(lbIndex + 1);
    });
    document.addEventListener('keydown', function(e){
      if (!lb || lb.hidden) return;
      if (e.key === 'Escape') closePreview();
      else if (e.key === 'ArrowLeft') showPreview(lbIndex - 1);
      else if (e.key === 'ArrowRight') showPreview(lbIndex + 1);
    });
  }
  function releaseMedia(){
    if(!lb) return;
    var holder = lb.querySelector('.lb-media');
    holder.querySelectorAll('audio,video').forEach(function(el){
      el.pause(); el.removeAttribute('src'); el.load();
    });
    holder.textContent = '';
  }
  function setMedia(item){
    releaseMedia();
    var holder = lb.querySelector('.lb-media');
    var el;
    if (item.kind === 'video') { el = document.createElement('video'); el.controls = true; }
    else if (item.kind === 'audio') { el = document.createElement('audio'); el.controls = true; }
    else { el = document.createElement('img'); el.alt = item.name; }
    el.src = window.canyouWithDid(item.link); // 真实路径带 did，媒体本体与页面一样过门禁
    holder.appendChild(el);
  }
  function showPreview(i){
    if (i < 0 || i >= media.length) return;
    if (!lb) buildLightbox();
    lbIndex = i;
    setMedia(media[i]);
    lbName.textContent = media[i].name;
    lbPos.textContent = media.length > 1 ? ((i + 1) + ' / ' + media.length) : '';
    lb.querySelector('.lb-prev').hidden = (i === 0);
    lb.querySelector('.lb-next').hidden = (i === media.length - 1);
    lb.hidden = false;
    lb.canyouOpen();
  }
  function closePreview(){
    if (!lb) return;
    if (lb.open) lb.close();
    lb.hidden = true;
    releaseMedia();
    lbIndex = -1;
  }
  function openPreview(p){
    for (var i = 0; i < media.length; i++){ if (media[i].path === p) { showPreview(i); return; } }
  }

  function render(data){
    current = data.path;
    renderBreadcrumbs();
    if (card) card.setAttribute('data-base', current);
    var items = data.items || [];
    lastCount = items.length;
    media = [];
    items.forEach(function(it){
      if (it.isDir) return;
      var k = kindOf(it.name);
      if (k === 'image' || k === 'video' || k === 'audio') media.push({name: it.name, path: it.path, link: it.link, kind: k});
    });
    closePreview();
    list.innerHTML = '';
    items.forEach(function(item){ list.appendChild(makeRow(item)); });
    if (!items.length){
      var empty = document.createElement('div'); empty.className = 'empty';
      empty.appendChild(window.canyouIcon('folder'));
      var label = document.createElement('div'); label.textContent = '这个文件夹还是空的'; empty.appendChild(label);
      list.appendChild(empty);
    }
    list.setAttribute('aria-busy', 'false');
    update();
    list.scrollTop = 0;
  }
  function showError(msg){
    lastCount = 0;
    media = [];
    closePreview();
    list.innerHTML = '';
    var box = document.createElement('div'); box.className = 'empty';
    var tip = document.createElement('div'); tip.textContent = msg;
    box.appendChild(tip);
    var retry = document.createElement('button'); retry.type = 'button'; retry.className = 'tbtn'; retry.style.marginTop = '16px'; retry.textContent = '重新加载';
    retry.addEventListener('click', function(){ show(requestedPath); }); box.appendChild(retry);
    if (requestedPath !== '/'){
      var btn = document.createElement('button'); btn.type = 'button'; btn.className = 'tbtn'; btn.style.marginTop = '12px';
      btn.textContent = '返回根目录';
      btn.addEventListener('click', function(){ navigate('/'); });
      box.appendChild(btn);
    }
    list.appendChild(box);
    list.setAttribute('aria-busy', 'false');
    update();
  }

  // 拉取并渲染目录；不碰历史栈，供初始化/前进后退/错误重试复用
  function show(p){
    requestedPath = p;
    var requestId = ++listRequest;
    closePreview();
    var pending = document.createElement('div'); pending.className = 'empty'; pending.textContent = '正在加载文件…';
    list.innerHTML = ''; list.appendChild(pending); list.setAttribute('aria-busy', 'true');
    lastCount = 0; update(); selHint.textContent = '正在加载…';
    fetch('/api/list', {method:'post', cache:'no-store',
      headers:{'Content-Type':'application/json','X-Device-ID':window.CANYOU_DID||''},
      body:JSON.stringify({path:p})})
      .then(function(resp){
        if (resp.status === 403) return {ok:false, message:'该设备尚未通过访问验证'};
        return resp.json().catch(function(){ return {ok:false, message:'服务器响应异常'}; });
      })
      .then(function(data){
        if (requestId !== listRequest) return;
        if (!data || !data.ok) { showError((data && data.message) || '加载失败'); return; }
        render(data);
        if (window.canyouUI) canyouUI({dir: data.path === '/' ? '' : data.path}); // 记住最近浏览目录，刷新后还原
      })
      .catch(function(){ if(requestId === listRequest) showError('加载失败：网络错误'); });
  }
  // 目录切换：pushState 不改 URL（恒为 /）只增历史栈，后退/前进可用
  function navigate(p){
    p = pathClean(p);
    if (window.history && history.pushState) history.pushState({p:p}, '', '/');
    show(p);
  }

  // 事件委托：动态渲染的行都在 #fileList 内，容器上只绑一次
  list.addEventListener('click', function(e){
    var el = e.target.closest ? e.target.closest('a') : null;
    if (!el) return;
    if (el.classList.contains('dlpick')) { e.preventDefault(); submitZip([el.getAttribute('data-path')]); return; } // 文件夹打包下载
    if (el.classList.contains('dl')) return; // 文件下载走真实路径，保持浏览器原生行为
    if (el.classList.contains('preview')) { e.preventDefault(); openPreview(el.getAttribute('data-path')); return; } // 媒体名：点击弹层预览
    var directory = el.getAttribute('data-directory');
    if (directory !== null) { e.preventDefault(); navigate(directory); return; }
    var parent = el.getAttribute('data-parent');
    if (parent !== null) { e.preventDefault(); navigate(parent); return; } // .. 行：回父目录
    var href = el.getAttribute('href') || '';
    if (href.charAt(0) === '#' || href.indexOf('?') !== -1) return; // 带 did 的下载链接不拦截
    e.preventDefault(); // 目录行：不再整页跳转，改走列表接口
    navigate(href);
  });
  list.addEventListener('change', function(e){ if (e.target.classList && e.target.classList.contains('pick')) update(); });
  selAll.addEventListener('click', function(){ boxes().forEach(function(b){ b.checked = true; }); update(); });
  selNone.addEventListener('click', function(){ boxes().forEach(function(b){ b.checked = false; }); update(); });
  zipBtn.addEventListener('click', function(){
    var items = checkedItems();
    if (!items.length) return;
    if (items.length === 1 && !items[0].dir) { // 仅选中单个文件时直接下载原文件
      var a = document.createElement('a');
      a.href = window.canyouWithDid(items[0].path); a.download = items[0].path.split('/').pop();
      document.body.appendChild(a); a.click(); a.remove();
      return;
    }
    submitZip(items.map(function(i){ return i.path; })); // 含文件夹或多项：打包
  });
  window.addEventListener('popstate', function(e){ show((e.state && e.state.p) || '/'); });

  // 初始化：旧目录 URL 作为初始目录后立即把地址栏清成 /；直接访根页时还原上次浏览的目录
  // location.pathname 是非 ASCII 的百分号编码串，单独还原；localStorage 里的目录本就是原文，不能再解
  function decodePath(p){
    try { return decodeURIComponent(String(p || '')); } catch (e) { return String(p || ''); } // 非法编码片段按原文处理
  }
  var initial = pathClean(decodePath(location.pathname));
  if (initial === '/' && window.canyouReadUI){
    var saved = canyouReadUI().dir;
    if (saved) initial = pathClean(saved);
  }
  if (window.history && history.replaceState) history.replaceState({p:initial}, '', '/');
  show(initial);
})();
