// Tabs of the console screenshots. Without JavaScript the first panel stays visible.
document.querySelectorAll<HTMLElement>('[data-showcase]').forEach((root) => {
  const tabs = Array.from(root.querySelectorAll<HTMLButtonElement>('[role="tab"]'));
  const panels = Array.from(root.querySelectorAll<HTMLElement>('[role="tabpanel"]'));

  function select(index: number, focus = false) {
    tabs.forEach((tab, i) => {
      const on = i === index;
      tab.setAttribute('aria-selected', String(on));
      tab.tabIndex = on ? 0 : -1;
      panels[i].hidden = !on;
    });
    if (focus) tabs[index].focus();
  }

  tabs.forEach((tab, i) => {
    tab.addEventListener('click', () => select(i));
    tab.addEventListener('keydown', (event) => {
      const last = tabs.length - 1;
      const next = event.key === 'ArrowRight' ? (i === last ? 0 : i + 1)
        : event.key === 'ArrowLeft' ? (i === 0 ? last : i - 1)
        : event.key === 'Home' ? 0
        : event.key === 'End' ? last
        : -1;
      if (next < 0) return;
      event.preventDefault();
      select(next, true);
    });
  });
});
