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

(element
  (start_tag) @start
  (end_tag)? @end) @indent

(start_tag
  ">" @end) @indent

(self_closing_tag
  "/>" @end) @indent

(html_case) @indent
