
(function(){
  var toggle = document.getElementById('uploadToggle');
  var panel = document.getElementById('uploadDialog');
  var closeBtn = document.getElementById('uploadClose');
  if (!toggle || !panel) return;
  window.canyouDialog(panel);
  function open(){ panel.canyouOpen(); window.canyouUI({upload:true}); }
  panel.addEventListener('close', function(){ if(!panel.open) window.canyouUI({upload:false}); });
  toggle.addEventListener('click', open);
  closeBtn.addEventListener('click', function(){ panel.close(); });
  panel.addEventListener('click', function(e){
    if(e.target !== panel) return;
    var r=panel.getBoundingClientRect();
    if(e.clientX<r.left || e.clientX>r.right || e.clientY<r.top || e.clientY>r.bottom) panel.close();
  });
  // 刷新只还原面板开合，浏览器不会恢复待上传文件。
  if (window.canyouReadUI().upload) open();
})();
