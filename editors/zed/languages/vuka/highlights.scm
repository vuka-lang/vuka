; Highlights for Vuka (.vuka), written against the tree-sitter-go grammar.
;
; A .vuka file is Go plus a few additions. tree-sitter-go parses the Go
; majority; its error recovery keeps most highlighting working across the
; Vuka-only constructs (JSX, @decorators, match, enum, field attributes) even
; though those are not valid Go. The `vuka lsp` language server provides the
; real intelligence (completion/hover/go-to-def/rename/diagnostics); these
; queries are the static coloring layer.

; ---------------------------------------------------------------------------
; Go (the majority of every .vuka file)
; ---------------------------------------------------------------------------

; Functions and methods
(function_declaration name: (identifier) @function)
(method_declaration name: (field_identifier) @function.method)
(call_expression function: (identifier) @function)
(call_expression function: (selector_expression field: (field_identifier) @function.method))

; Types
(type_identifier) @type
(type_spec name: (type_identifier) @type)
((type_identifier) @type.builtin
  (#match? @type.builtin "^(any|bool|byte|comparable|complex64|complex128|error|float32|float64|int|int8|int16|int32|int64|rune|string|uint|uint8|uint16|uint32|uint64|uintptr)$"))

; Fields, packages, properties
(field_identifier) @property
(keyed_element (literal_element (identifier) @property))
(package_identifier) @namespace
(selector_expression field: (field_identifier) @property)

; Parameters / variables
(parameter_declaration (identifier) @variable.parameter)
(variadic_parameter_declaration (identifier) @variable.parameter)
(identifier) @variable

; Constants
(const_spec name: (identifier) @constant)
((identifier) @constant
  (#match? @constant "^[A-Z][A-Z0-9_]+$"))
(iota) @constant.builtin
((identifier) @constant.builtin
  (#match? @constant.builtin "^(nil|true|false)$"))
(true) @boolean
(false) @boolean
(nil) @constant.builtin

; Literals
(interpreted_string_literal) @string
(raw_string_literal) @string
(rune_literal) @string
(escape_sequence) @string.escape
(int_literal) @number
(float_literal) @number
(imaginary_literal) @number

; Comments
(comment) @comment

; Operators
[
  "+" "-" "*" "/" "%"
  "&" "|" "^" "&^" "<<" ">>"
  "+=" "-=" "*=" "/=" "%="
  "&=" "|=" "^=" "&^=" "<<=" ">>="
  "&&" "||" "!"
  "==" "!=" "<" "<=" ">" ">="
  "=" ":=" "..." "<-"
  "++" "--"
] @operator

; Punctuation
[ "(" ")" "{" "}" "[" "]" ] @punctuation.bracket
[ "." "," ";" ":" ] @punctuation.delimiter

; Keywords
[
  "break" "case" "chan" "const" "continue" "default" "defer" "else"
  "fallthrough" "for" "func" "go" "goto" "if" "import" "interface" "map"
  "package" "range" "return" "select" "struct" "switch" "type" "var"
] @keyword

(label_name) @label

; ---------------------------------------------------------------------------
; Vuka additions (best-effort against tree-sitter-go's recovered parse)
; ---------------------------------------------------------------------------

; Result / Option — the Vuka runtime result types.
((type_identifier) @type.builtin
  (#match? @type.builtin "^(Result|Option)$"))
((identifier) @type.builtin
  (#match? @type.builtin "^(Result|Option)$"))

; Ok(…) / Err(…) / Some(…) constructors and the None constant.
((call_expression function: (identifier) @function.builtin)
  (#match? @function.builtin "^(Ok|Err|Some)$"))
((identifier) @constant.builtin
  (#eq? @constant.builtin "None"))

; match / enum / decorator / static — Vuka keywords. They are not Go keywords,
; so they surface as identifiers in the recovered parse; color them as keywords
; when they appear as bare words. (Reliability varies with error recovery; the
; LSP is authoritative.)
((identifier) @keyword
  (#match? @keyword "^(match|enum|decorator|static)$"))

; Self / self — the receiver in static/overloaded methods.
((identifier) @variable.special
  (#match? @variable.special "^(Self|self)$"))
