// key.js — Keyboard shortcut bindings

const Keys = (() => {
  const handlers = {};

  document.addEventListener('keydown', (e) => {
    // Skip when typing in inputs
    if (e.target.tagName === 'INPUT' || e.target.tagName === 'TEXTAREA' || e.target.isContentEditable) {
      return;
    }

    // Skip while a modal dialog is open — shortcuts would act on the
    // list/article behind it (select elements are not caught by the check above)
    if (document.querySelector('dialog[open]')) {
      return;
    }

    const key = [
      e.ctrlKey ? 'ctrl+' : '',
      e.metaKey ? 'meta+' : '',
      e.shiftKey ? 'shift+' : '',
      e.key,
    ].join('');

    const handler = handlers[key] || handlers[e.key];
    if (handler) {
      e.preventDefault();
      handler(e);
    }
  });

  function bind(key, fn) {
    handlers[key] = fn;
  }

  return { bind };
})();
