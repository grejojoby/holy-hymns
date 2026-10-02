(() => {
  const tabs = [...document.querySelectorAll('[data-script]')];
  const toolbar = document.querySelector('.reader-toolbar');
  if (tabs.length && toolbar) {
    toolbar.hidden = false;
    const select = tab => tabs.forEach(button => {
      const active = button === tab;
      button.setAttribute('aria-selected', String(active));
      button.tabIndex = active ? 0 : -1;
      const panel = document.getElementById(button.dataset.script);
      panel.hidden = !active;
      panel.setAttribute('role', 'tabpanel');
      panel.setAttribute('aria-labelledby', button.id);
      panel.tabIndex = 0;
    });
    tabs.forEach((tab, index) => {
      tab.addEventListener('click', () => select(tab));
      tab.addEventListener('keydown', event => {
        let next;
        if (event.key === 'ArrowRight') next = (index + 1) % tabs.length;
        if (event.key === 'ArrowLeft') next = (index + tabs.length - 1) % tabs.length;
        if (event.key === 'Home') next = 0;
        if (event.key === 'End') next = tabs.length - 1;
        if (next !== undefined) { event.preventDefault(); select(tabs[next]); tabs[next].focus(); }
      });
    });
    select(tabs[0]);
    let size = 20;
    const controls = [...document.querySelectorAll('[data-size]')];
    controls.forEach(button => button.addEventListener('click', () => {
      size = Math.max(16, Math.min(36, size + Number(button.dataset.size) * 2));
      document.querySelector('.lyrics-paper').style.setProperty('--lyric-size', `${size / 16}rem`);
      controls.forEach(control => { control.disabled = Number(control.dataset.size) < 0 ? size === 16 : size === 36; });
    }));
  }
  const copy = document.querySelector('.copy-link');
  if (copy && navigator.clipboard) {
    copy.hidden = false;
    copy.addEventListener('click', async () => {
      const status = document.querySelector('.copy-status');
      try { await navigator.clipboard.writeText(document.querySelector('link[rel="canonical"]').href); status.textContent = 'Hymn link copied.'; }
      catch { status.textContent = 'Copy the address from your browser to share this hymn.'; }
    });
  }
})();
