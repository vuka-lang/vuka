/**
 * @file Vuka grammar for tree-sitter: Go, and Vuka's additions to it.
 * @license MIT
 */

/// <reference types="tree-sitter-cli/dsl" />
// @ts-check

const go = require('tree-sitter-go/grammar');

const PREC = {
  primary: 7,
};

const terminator = choice('\n', ';', '\0');

module.exports = grammar(go, {
  name: 'vuka',

  // The .vuka.html grammar shares this scanner, and so its tokens.
  externals: $ => [
    $._markup_text,
    $._block_text,
    $.raw_text,
    $._html_text,
    $._close_brace,
    $._error_sentinel,
  ],

  conflicts: ($, previous) => previous.concat([
    [$.type_parameter_declaration, $._simple_type],
    [$.match_statement, $._contextual_keyword],
    [$._contextual_keyword, $.markup_match],
  ]),

  rules: {
    source_file: $ => seq(
      repeat(choice(
        seq($._statement, terminator),
        seq($._top_level_declaration, terminator),
        seq($.attribute, optional(terminator)),
      )),
      optional($._top_level_declaration),
    ),

    _top_level_declaration: ($, previous) => choice(
      previous,
      $.enum_declaration,
      $.decorator_declaration,
    ),

    attribute: $ => prec.right(seq(
      '@',
      field('name', $.attribute_name),
      optional(field('type_arguments', $.type_arguments)),
      optional(choice(
        field('arguments', $.argument_list),
        field('value', $.literal_value),
      )),
      repeat(seq('.', field('method', $._field_identifier), $.argument_list)),
    )),

    attribute_name: $ => prec.right(seq(
      optional(seq(field('package', $._package_identifier), '.')),
      $.identifier,
    )),

    function_declaration: $ => prec.right(1, seq(
      'func',
      optional(seq(
        field('static_type', $._type_identifier),
        optional(field('static_type_arguments', $.type_arguments)),
        '.',
      )),
      field('name', $.identifier),
      field('type_parameters', optional($.type_parameter_list)),
      field('parameters', $.parameter_list),
      field('result', optional(choice($.parameter_list, $._simple_type))),
      field('body', optional($.block)),
    )),

    parameter_declaration: $ => seq(
      repeat($.attribute),
      commaSep(field('name', $.identifier)),
      field('type', $._type),
    ),

    field_declaration: $ => seq(
      choice(
        seq(
          commaSep1(field('name', choice($._field_identifier, alias('static', $.field_identifier)))),
          field('type', $._type),
        ),
        seq(
          optional('*'),
          field('type', choice(
            $._type_identifier,
            $.qualified_type,
            $.generic_type,
          )),
        ),
      ),
      field('tag', optional($._string_literal)),
      repeat($.attribute),
    ),

    field_declaration_list: $ => seq(
      '{',
      optional(seq(
        choice($.field_declaration, $.static_declaration),
        repeat(seq(terminator, choice($.field_declaration, $.static_declaration))),
        optional(terminator),
      )),
      '}',
    ),

    static_declaration: $ => seq(
      'static',
      optional('const'),
      field('name', $._field_identifier),
      choice(
        seq(field('type', $._type), optional(seq('=', field('value', $._expression)))),
        seq('=', field('value', $._expression)),
      ),
    ),

    enum_declaration: $ => seq(
      'enum',
      field('name', $._type_identifier),
      optional(field('type', $._type)),
      '{',
      optional(seq(
        $.enum_variant,
        repeat(seq(choice(',', terminator), $.enum_variant)),
        optional(choice(',', terminator)),
      )),
      '}',
    ),

    enum_variant: $ => seq(
      field('name', $.identifier),
      optional(choice(
        seq('=', field('value', $._expression)),
        field('fields', $.field_declaration_list),
      )),
    ),

    decorator_declaration: $ => seq(
      'decorator',
      field('name', $.identifier),
      choice(
        seq(
          optional(field('parameters', $.parameter_list)),
          '=',
          choice(
            repeat1($.attribute),
            seq('(', repeat(choice($.attribute, terminator)), ')'),
          ),
        ),
        seq(
          field('parameters', $.parameter_list),
          optional(field('call', $.parameter_list)),
          field('body', $.block),
        ),
      ),
    ),

    _statement: ($, previous) => choice(previous, $.match_statement),

    match_statement: $ => seq(
      'match',
      optional(seq(field('initializer', $._simple_statement), ';')),
      field('value', $._expression),
      '{',
      repeat(choice($.match_case, $.default_case)),
      '}',
    ),

    match_case: $ => seq(
      'case',
      field('pattern', $.expression_list),
      optional(seq('if', field('guard', $._expression))),
      ':',
      optional($.statement_list),
    ),

    _expression: ($, previous) => choice(
      previous,
      $.try_expression,
      $.markup_element,
      alias($._contextual_keyword, $.identifier),
    ),

    _contextual_keyword: _ => choice('match', 'enum', 'static', 'decorator'),

    try_expression: $ => prec.right(PREC.primary, seq(
      field('operand', $._expression),
      '?',
      optional(field('message', $.argument_list)),
    )),

    markup_element: $ => choice(
      $.markup_raw_element,
      seq(
        field('open', $.markup_start_tag),
        repeat($._markup_child),
        field('close', $.markup_end_tag),
      ),
      $.markup_self_closing_tag,
    ),

    markup_start_tag: $ => seq(
      '<',
      optional(seq(
        field('name', $.tag_name),
        optional(field('type_arguments', $.type_arguments)),
        repeat($._markup_attribute),
      )),
      '>',
    ),

    markup_self_closing_tag: $ => seq(
      '<',
      field('name', $.tag_name),
      optional(field('type_arguments', $.type_arguments)),
      repeat($._markup_attribute),
      '/>',
    ),

    markup_end_tag: $ => seq(
      '</',
      optional(seq(
        field('name', $.tag_name),
        optional(field('type_arguments', $.type_arguments)),
      )),
      '>',
    ),

    markup_raw_element: $ => seq(
      '<',
      field('name', alias($._raw_tag_name, $.tag_name)),
      repeat($._markup_attribute),
      '>',
      optional(field('content', $.raw_text)),
      '</',
      field('close_name', alias($._raw_tag_name, $.tag_name)),
      '>',
    ),

    _raw_tag_name: _ => choice('script', 'style'),

    tag_name: _ => /[A-Za-z_][\w:-]*(\.[A-Za-z_][\w:-]*)*/,

    _markup_attribute: $ => choice($.markup_attribute, $.markup_expression),

    markup_attribute: $ => seq(
      field('name', $.markup_attribute_name),
      optional(seq('=', field('value', choice($.markup_string, $.markup_expression)))),
    ),

    markup_attribute_name: _ => /[A-Za-z_@:][\w.:-]*/,

    markup_string: _ => choice(/"[^"]*"/, /'[^']*'/),

    _markup_child: $ => choice(
      $.markup_element,
      $.markup_expression,
      alias($._markup_text, $.markup_text),
    ),

    markup_expression: $ => seq(
      '{',
      optional(choice(
        $._markup_hole,
        $.markup_if,
        $.markup_for,
        $.markup_match,
      )),
      '}',
    ),

    _markup_hole: $ => choice(
      seq($._expression, repeat(seq(',', $._expression))),
      $.variadic_argument,
      seq(
        $._markup_statement,
        repeat(seq(';', choice($._markup_statement, $._expression))),
        optional(';'),
      ),
    ),

    _markup_statement: $ => choice(
      $.assignment_statement,
      $.inc_statement,
      $.dec_statement,
    ),

    markup_if: $ => seq(
      'if',
      optional(seq(field('initializer', $._simple_statement), ';')),
      field('condition', $._expression),
      field('consequence', $.markup_block),
      optional(seq(
        'else',
        field('alternative', choice($.markup_block, $.markup_if)),
      )),
    ),

    markup_for: $ => seq(
      'for',
      optional(choice($._expression, $.for_clause, $.range_clause)),
      field('body', $.markup_block),
    ),

    markup_match: $ => seq(
      'match',
      field('value', $._expression),
      '{',
      repeat($.markup_case),
      '}',
    ),

    markup_case: $ => seq(
      choice(
        seq(
          'case',
          field('pattern', $.expression_list),
          optional(seq('if', field('guard', $._expression))),
        ),
        'default',
      ),
      ':',
      repeat($._markup_block_child),
    ),

    markup_block: $ => seq(
      '{',
      repeat($._markup_block_child),
      '}',
    ),

    _markup_block_child: $ => choice(
      $.markup_element,
      $.markup_expression,
      $.markup_if,
      $.markup_for,
      $.markup_match,
      alias($._block_text, $.markup_text),
    ),
  },
});

function commaSep1(rule) {
  return seq(rule, repeat(seq(',', rule)));
}

function commaSep(rule) {
  return optional(commaSep1(rule));
}
