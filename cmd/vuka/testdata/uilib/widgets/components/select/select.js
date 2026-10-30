(() => {
  "use strict";
  document.addEventListener("click", (e) => {
    const t = e.target instanceof Element ? e.target : null;
    if (!t) return;
    const opener = t.closest("[data-select-trigger]");
    if (opener) {
      const id = opener.getAttribute("data-select-trigger");
      window.widgets.show(document.getElementById(id + "-list").closest("[data-select-portal]"));
      opener.setAttribute("aria-expanded", "true");
      return;
    }
    const option = t.closest('[role="option"]');
    if (!option) return;
    const list = option.closest('[role="listbox"]'), id = list.id.replace(/-list$/, "");
    for (const o of list.querySelectorAll('[role="option"]')) o.setAttribute("aria-selected", String(o === option));
    const trig = document.getElementById(id + "-trigger");
    trig.querySelector("[data-select-value]").textContent = option.textContent.trim();
    trig.setAttribute("aria-expanded", "false");
    document.getElementById(id + "-input").value = option.getAttribute("data-value");
    window.widgets.hide(list.closest("[data-select-portal]"));
  });
})();
