// While open, the list's positioner has a backdrop before it, in the
// portal, and focus guards stand where the list was declared; closing
// plays an animation, then takes both out by the references it kept, as
// the select of a library built on Base UI does.
(() => {
  "use strict";
  const CLOSE_MS = 300;

  function open(trigger) {
    const id = trigger.getAttribute("data-select-trigger");
    const list = document.getElementById(id + "-list"), pos = list.parentElement, portal = pos.parentElement;
    clearTimeout(pos._closing);
    pos.removeAttribute("data-ending-style");
    if (!portal.hidden) return;
    const home = portal.parentNode === document.body ? pos._home : portal.parentElement;
    pos._home = home;
    window.widgets.show(portal);
    pos.hidden = false;
    pos._backdrop = document.createElement("div");
    pos._backdrop.setAttribute("data-select-backdrop", "");
    pos.before(pos._backdrop);
    pos._guards = [0, 1].map(() => {
      const g = document.createElement("span");
      g.setAttribute("data-focus-guard", "");
      g.tabIndex = 0;
      return g;
    });
    home.before(pos._guards[0]);
    home.after(pos._guards[1]);
    trigger.setAttribute("aria-expanded", "true");
  }

  function close(list) {
    const pos = list.parentElement, portal = pos.parentElement;
    pos._backdrop.inert = true;
    pos.setAttribute("data-ending-style", "");
    pos._closing = setTimeout(() => {
      pos.removeAttribute("data-ending-style");
      pos.hidden = true;
      window.widgets.hide(portal);
      pos._backdrop.remove();
      pos._guards.forEach((g) => g.remove());
    }, CLOSE_MS);
  }

  document.addEventListener("click", (e) => {
    const t = e.target instanceof Element ? e.target : null;
    if (!t) return;
    const opener = t.closest("[data-select-trigger]");
    if (opener) return open(opener);
    const option = t.closest('[role="option"]');
    if (!option) return;
    const list = option.closest('[role="listbox"]'), id = list.id.replace(/-list$/, "");
    for (const o of list.querySelectorAll('[role="option"]')) o.setAttribute("aria-selected", String(o === option));
    const trig = document.getElementById(id + "-trigger");
    trig.querySelector("[data-select-value]").textContent = option.textContent.trim();
    trig.setAttribute("aria-expanded", "false");
    document.getElementById(id + "-input").value = option.getAttribute("data-value");
    close(list);
  });
})();
