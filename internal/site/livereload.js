(function () {
  var u = new URL(__RELOAD_PATH__,window.location.href);
  u.protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
  var overlay = null;
  var disconnected = false;

  function hide() {
    if (overlay) {
      overlay.remove();
      overlay = null;
    }
  }

  function el(tag, css, text) {
    var node = document.createElement(tag);
    node.style.cssText = css;
    if (text !== undefined) node.textContent = text;
    return node;
  }

  function show(message) {
    hide();
    overlay = el('div', 'position:fixed;top:0;right:0;bottom:0;left:0;z-index:2147483647;overflow:auto;box-sizing:border-box;padding:5vh 5vw;background:rgba(0,0,0,.66);font:14px/1.5 ui-monospace,SFMono-Regular,Menlo,Consolas,monospace');
    overlay.setAttribute('data-kopkop-error', '');
    var card = el('div', 'max-width:960px;margin:0 auto;padding:20px 24px;border-top:4px solid #ff5555;border-radius:6px;background:#1b1b1f;color:#e6e6e6;box-shadow:0 8px 40px rgba(0,0,0,.5)');
    card.appendChild(el('div', 'margin-bottom:12px;color:#ff5555;font-size:16px;font-weight:700', 'Build error'));
    card.appendChild(el('pre', 'margin:0;white-space:pre-wrap;word-break:break-word', message));
    card.appendChild(el('div', 'margin-top:16px;color:#8b8b95;font-size:12px', 'Fix the error and save: this page reloads automatically. Click outside this box or press Esc to dismiss.'));
    overlay.appendChild(card);
    overlay.addEventListener('click', function (e) {
      if (e.target === overlay) hide();
    });
    document.body.appendChild(overlay);
  }

  document.addEventListener('keydown', function (e) {
    if (e.key === 'Escape') hide();
  });

  function connect() {
    var ws = new WebSocket(u);
    ws.onopen = function () {
      // The server went away and came back: its output may have changed.
      if (disconnected) window.location.reload();
    };
    ws.onmessage = function (e) {
      if (e.data === 'reload') {
        window.location.reload();
        return;
      }
      try {
        var m = JSON.parse(e.data);
        if (m.type === 'error') show(m.message);
      } catch (_) {}
    };
    ws.onclose = function () {
      disconnected = true;
      setTimeout(connect, 1000);
    };
  }
  connect();
})();
