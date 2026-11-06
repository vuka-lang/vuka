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

((rune_literal) @open @close
  (#set! rainbow.exclude))

((markup_start_tag
  "<" @open
  ">" @close)
  (#set! rainbow.exclude))

((markup_end_tag
  "</" @open
  ">" @close)
  (#set! rainbow.exclude))

((markup_self_closing_tag
  "<" @open
  "/>" @close)
  (#set! rainbow.exclude))
