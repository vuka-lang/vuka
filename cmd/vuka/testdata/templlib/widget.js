(() => {
  // A popup goes to the body while open, as overlay libraries portal it,
  // and stays there, hidden, once closed.
  const show = (portal) => {
    if (portal.parentNode !== document.body) document.body.appendChild(portal);
    portal.hidden = false;
    portal.setAttribute("data-open", "");
  };
  const hide = (portal) => {
    portal.hidden = true;
    portal.removeAttribute("data-open");
  };
  document.addEventListener("click", (e) => {
    const t = e.target instanceof Element ? e.target : null;
    if (!t) return;
    const trigger = t.closest("[data-dialog-trigger]");
    if (trigger) {
      const dialog = document.getElementById(trigger.getAttribute("data-dialog-trigger"));
      if (!dialog) return;
      show(dialog.closest("[data-dialog-portal]"));
      trigger.setAttribute("aria-expanded", "true");
      return;
    }
    if (t.closest("[data-dialog-close]")) {
      hide(t.closest("[data-dialog-portal]"));
      return;
    }
    const opener = t.closest("[data-select-trigger]");
    if (opener) {
      const id = opener.getAttribute("data-select-trigger");
      show(document.getElementById(id + "-list").closest("[data-select-portal]"));
      opener.setAttribute("aria-expanded", "true");
      return;
    }
    const option = t.closest('[role="option"]');
    if (option) {
      const list = option.closest('[role="listbox"]'), id = list.id.replace(/-list$/, "");
      const value = option.getAttribute("data-value");
      for (const o of list.querySelectorAll('[role="option"]')) o.setAttribute("aria-selected", String(o === option));
      const trig = document.getElementById(id + "-trigger");
      trig.querySelector("[data-select-value]").textContent = option.textContent.trim();
      trig.setAttribute("aria-expanded", "false");
      document.getElementById(id + "-input").value = value;
      hide(list.closest("[data-select-portal]"));
    }
  });
})();
