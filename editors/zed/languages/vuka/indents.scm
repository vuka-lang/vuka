[
  (assignment_statement)
  (call_expression)
  (selector_expression)
] @indent

(_
  "["
  "]" @end) @indent

(_
  "{"
  "}" @end) @indent

(_
  "("
  ")" @end) @indent

(markup_element
  (markup_start_tag) @start
  (markup_end_tag)? @end) @indent

(markup_start_tag
  ">" @end) @indent

(markup_self_closing_tag
  "/>" @end) @indent
