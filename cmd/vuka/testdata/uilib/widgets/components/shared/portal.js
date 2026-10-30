// The popups' portal, which the components' scripts share: a popup goes to
// the body while open, and stays there, hidden, once closed.
(() => {
  "use strict";
  window.widgets = {
    show(portal) {
      if (portal.parentNode !== document.body) document.body.appendChild(portal);
      portal.hidden = false;
      portal.setAttribute("data-open", "");
    },
    hide(portal) {
      portal.hidden = true;
      portal.removeAttribute("data-open");
    },
  };
})();
