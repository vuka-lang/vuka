; The shape's Go is its <script lang="go">: highlighted as Go. A component's
; <script lang="ts"> is TypeScript, any other script JavaScript, and a
; <style> CSS, as the HTML grammar has them.
(script_element
  (start_tag
    (attribute
      (attribute_name) @_name
      (quoted_attribute_value (attribute_value) @_lang)))
  (raw_text) @injection.content
  (#eq? @_name "lang")
  (#eq? @_lang "go")
  (#set! injection.language "go"))

(script_element
  (start_tag
    (attribute
      (attribute_name) @_name
      (quoted_attribute_value (attribute_value) @_lang)))
  (raw_text) @injection.content
  (#eq? @_name "lang")
  (#eq? @_lang "ts")
  (#set! injection.language "typescript"))

(style_element
  (raw_text) @injection.content
  (#set! injection.language "css"))
