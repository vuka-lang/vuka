A stand-in for a templ component library (shadcn-templ, templUI), laid out
as one is in its Go module: a package per component under components/, a
helper package (utils), scripts sharing a directory of their own
(components/shared), the theme, stylesheets and a style of the components'
classes under assets/css. vuka ui add widgets/<name> reads it through
vuka.toml's [ui.libraries.widgets] dir.
