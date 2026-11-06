((comment) @injection.content
  (#set! injection.language "comment"))

(markup_raw_element
  (tag_name) @_tag
  (markup_attribute
    name: (markup_attribute_name) @_name
    value: (markup_string) @_lang)
  (raw_text) @injection.content
  (#eq? @_tag "script")
  (#eq? @_name "lang")
  (#match? @_lang "^[\"']ts[\"']$")
  (#set! injection.language "typescript"))

(markup_raw_element
  (tag_name) @_tag
  .
  (raw_text) @injection.content
  (#eq? @_tag "script")
  (#set! injection.language "javascript"))

(markup_raw_element
  (tag_name) @_tag
  (raw_text) @injection.content
  (#eq? @_tag "style")
  (#set! injection.language "css"))
