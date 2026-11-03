// Marks a plain form's submit button aria-busy while the browser posts it,
// so it shows its spinner (a page restored from the back-forward cache
// clears it), and keeps the fields marked data-keep for this tab, so a
// form that comes back with an error still has them.
addEventListener("submit", (e) => {
  if (e.defaultPrevented) return;
  const b = e.submitter || e.target.querySelector("[type=submit]");
  if (b) b.setAttribute("aria-busy", "true");
  e.target.querySelectorAll("[data-keep]").forEach((f) => sessionStorage.setItem("keep:" + f.name, f.value));
});
addEventListener("DOMContentLoaded", () => document.querySelectorAll("[data-keep]").forEach((f) => {
  if (!f.value) f.value = sessionStorage.getItem("keep:" + f.name) || "";
}));
addEventListener("pageshow", () => document.querySelectorAll("[aria-busy=true]").forEach((b) => b.removeAttribute("aria-busy")));
