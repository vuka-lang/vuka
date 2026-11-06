((comment) @injection.content
  (#set! injection.language "comment"))

(raw_element
  (tag_name) @_tag
  (html_attribute
    name: (attribute_name) @_name
    value: (quoted_value (value_text) @_lang))
  (raw_text) @injection.content
  (#eq? @_tag "script")
  (#eq? @_name "lang")
  (#eq? @_lang "go")
  (#set! injection.language "vuka"))

(raw_element
  (tag_name) @_tag
  (html_attribute
    name: (attribute_name) @_name
    value: (quoted_value (value_text) @_lang))
  (raw_text) @injection.content
  (#eq? @_tag "script")
  (#eq? @_name "lang")
  (#eq? @_lang "ts")
  (#set! injection.language "typescript"))

(raw_element
  (tag_name) @_tag
  .
  (raw_text) @injection.content
  (#eq? @_tag "script")
  (#set! injection.language "javascript"))

(raw_element
  (tag_name) @_tag
  (raw_text) @injection.content
  (#eq? @_tag "style")
  (#set! injection.language "css"))
