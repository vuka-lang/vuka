(() => {
  "use strict";
  document.addEventListener("click", (e) => {
    const t = e.target instanceof Element ? e.target : null;
    if (!t) return;
    const trigger = t.closest("[data-dialog-trigger]");
    if (trigger) {
      const dialog = document.getElementById(trigger.getAttribute("data-dialog-trigger"));
      if (!dialog) return;
      window.widgets.show(dialog.closest("[data-dialog-portal]"));
      trigger.setAttribute("aria-expanded", "true");
      return;
    }
    if (t.closest("[data-dialog-close]")) window.widgets.hide(t.closest("[data-dialog-portal]"));
  });
})();
