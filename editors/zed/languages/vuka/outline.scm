; Outline for Vuka (.vuka), against tree-sitter-go. Covers the Go declarations;
; Vuka-only declarations (enum, static, decorator) are not valid Go and so are
; not reliably reachable here — use the `vuka lsp` document symbols
; (go-to-symbol) for the full, accurate outline.

(function_declaration
  "func" @context
  name: (identifier) @name) @item

(method_declaration
  "func" @context
  receiver: (parameter_list) @context
  name: (field_identifier) @name) @item

(type_declaration
  "type" @context
  (type_spec name: (type_identifier) @name)) @item

(const_declaration
  "const" @context
  (const_spec name: (identifier) @name)) @item

(var_declaration
  "var" @context
  (var_spec name: (identifier) @name)) @item
