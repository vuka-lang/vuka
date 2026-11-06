(function_declaration
  body: (_
    "{"
    (_)* @function.inside
    "}")) @function.around

(method_declaration
  body: (_
    "{"
    (_)* @function.inside
    "}")) @function.around

(type_declaration) @class.around

(element
  (start_tag)
  (_)* @class.inside
  (end_tag)) @class.around

[
  (comment)
  (html_comment)
]+ @comment.around
