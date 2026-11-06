(comment) @annotation

(type_declaration
  "type" @context
  [
    (type_spec
      name: (_) @name) @item
    ("("
      (type_spec
        name: (_) @name) @item
      ")")
  ])

(enum_declaration
  "enum" @context
  name: (_) @name) @item

(function_declaration
  "func" @context
  name: (identifier) @name
  parameters: (parameter_list
    "("
    ")")) @item

(method_declaration
  "func" @context
  receiver: (parameter_list
    "(" @context
    (parameter_declaration
      name: (_)? @context
      type: (_) @context)
    ")" @context)
  name: (field_identifier) @name
  parameters: (parameter_list
    "("
    ")")) @item

(template_header
  "func" @context
  name: (identifier) @name) @item

(field_declaration
  name: (_) @name @item)

(raw_element
  (tag_name) @name) @item
