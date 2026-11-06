("(" @open
  ")" @close)

("[" @open
  "]" @close)

("{" @open
  "}" @close)

(("\"" @open
  "\"" @close)
  (#set! rainbow.exclude))

(("`" @open
  "`" @close)
  (#set! rainbow.exclude))

((start_tag
  "<" @open
  ">" @close)
  (#set! rainbow.exclude))

((end_tag
  "</" @open
  ">" @close)
  (#set! rainbow.exclude))

((self_closing_tag
  "<" @open
  "/>" @close)
  (#set! rainbow.exclude))
