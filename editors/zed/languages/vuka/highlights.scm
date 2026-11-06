; Go

(identifier) @variable

(type_identifier) @type

(type_spec
  name: (type_identifier) @type.definition)

((type_identifier) @type.builtin
  (#any-of? @type.builtin
    "any" "bool" "byte" "comparable" "complex64" "complex128" "error" "float32" "float64" "int"
    "int8" "int16" "int32" "int64" "rune" "string" "uint" "uint8" "uint16" "uint32" "uint64"
    "uintptr"))

(type_parameter_declaration
  name: (identifier) @type)

(field_identifier) @property

(package_identifier) @namespace

(label_name) @label

(keyed_element
  .
  (literal_element
    (identifier) @property))

(call_expression
  function: (identifier) @function.call)

(call_expression
  function: (selector_expression
    field: (field_identifier) @function.method.call))

((call_expression
  function: (identifier) @function.builtin)
  (#any-of? @function.builtin
    "append" "cap" "clear" "close" "complex" "copy" "delete" "imag" "len" "make" "max" "min" "new"
    "panic" "print" "println" "real" "recover"))

(function_declaration
  name: (identifier) @function)

(method_declaration
  name: (field_identifier) @function.method)

(method_elem
  name: (field_identifier) @function.method)

[
  ";"
  "."
  ","
  ":"
] @punctuation.delimiter

[
  "("
  ")"
  "{"
  "}"
  "["
  "]"
] @punctuation.bracket

[
  "--"
  "-"
  "-="
  ":="
  "!"
  "!="
  "..."
  "*"
  "*="
  "/"
  "/="
  "&"
  "&&"
  "&="
  "%"
  "%="
  "^"
  "^="
  "+"
  "++"
  "+="
  "<-"
  "<"
  "<<"
  "<<="
  "<="
  "="
  "=="
  ">"
  ">="
  ">>"
  ">>="
  "|"
  "|="
  "||"
  "~"
] @operator

[
  "chan"
  "const"
  "func"
  "import"
  "interface"
  "map"
  "package"
  "struct"
  "type"
  "var"
] @keyword

[
  "break"
  "case"
  "continue"
  "default"
  "defer"
  "else"
  "fallthrough"
  "for"
  "go"
  "goto"
  "if"
  "range"
  "return"
  "select"
  "switch"
] @keyword.control

[
  (interpreted_string_literal)
  (raw_string_literal)
  (rune_literal)
] @string

(escape_sequence) @string.escape

[
  (int_literal)
  (float_literal)
  (imaginary_literal)
] @number

(const_spec
  name: (identifier) @constant)

[
  (true)
  (false)
] @boolean

[
  (nil)
  (iota)
] @constant.builtin

(comment) @comment

((comment) @preproc
  (#match? @preproc "^//go:"))

; Vuka

[
  "decorator"
  "enum"
  "static"
] @keyword

"match" @keyword.control

(try_expression
  "?" @operator)

(attribute
  "@" @attribute)

(attribute
  name: (attribute_name
    (identifier) @attribute))

(attribute
  method: (field_identifier) @function.method.call)

(enum_declaration
  name: (type_identifier) @type.definition)

(enum_variant
  name: (identifier) @variant)

(decorator_declaration
  name: (identifier) @function)

(static_declaration
  name: (field_identifier) @property)

(function_declaration
  static_type: (type_identifier) @type)

((type_identifier) @type.builtin
  (#any-of? @type.builtin "Result" "Option"))

((call_expression
  function: (identifier) @constructor)
  (#any-of? @constructor "Ok" "Err" "Some"))

((identifier) @constant.builtin
  (#eq? @constant.builtin "None"))

((identifier) @variable.special
  (#any-of? @variable.special "self" "Self"))

; Markup

(tag_name) @tag

((tag_name) @type
  (#match? @type "^[A-Z]|\\."))

[
  (markup_attribute_name)
] @attribute

(markup_string) @string

(markup_start_tag
  [
    "<"
    ">"
  ] @punctuation.bracket)

(markup_end_tag
  [
    "</"
    ">"
  ] @punctuation.bracket)

(markup_self_closing_tag
  [
    "<"
    "/>"
  ] @punctuation.bracket)

(markup_raw_element
  [
    "<"
    ">"
    "</"
  ] @punctuation.bracket)

(markup_attribute
  "=" @punctuation.delimiter)

(markup_expression
  [
    "{"
    "}"
  ] @punctuation.special)
