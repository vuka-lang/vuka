(function () {
  var root = document.documentElement
  function sync() {
    var dark = false
    try { dark = parent.document.documentElement.classList.contains('dark') } catch (e) {}
    root.dataset.theme = dark ? 'dark' : 'light'
  }
  sync()
  try { new MutationObserver(sync).observe(parent.document.documentElement, { attributes: true, attributeFilter: ['class'] }) } catch (e) {}
})()
