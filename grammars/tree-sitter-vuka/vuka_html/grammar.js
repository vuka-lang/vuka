/**
 * @file Vuka HTML grammar for tree-sitter: .vuka.html shapes and templates.
 * Markup with Go in its holes and statement lines, after an optional Go
 * header; the Go is Vuka's grammar.
 * @license MIT
 */

/// <reference types="tree-sitter-cli/dsl" />
// @ts-check

const vuka = require('../vuka/grammar');

const terminator = choice('\n', ';', '\0');

const voidElements = [
  'area', 'base', 'br', 'col', 'embed', 'hr', 'img', 'input',
  'link', 'meta', 'param', 'source', 'track', 'wbr',
];

module.exports = grammar(vuka, {
  name: 'vuka_html',

  rules: {
    source_file: $ => seq(
      repeat(choice(
        seq($._header_declaration, terminator),
        seq($.attribute, terminator),
      )),
      repeat($._node),
    ),

    _header_declaration: $ => choice(
      $.package_clause,
      $.import_declaration,
      $.const_declaration,
      $.type_declaration,
      $.var_declaration,
      $.function_declaration,
      $.method_declaration,
      $.enum_declaration,
      $.decorator_declaration,
    ),

    _node: $ => choice(
      $.element,
      $.raw_element,
      $.html_comment,
      $.doctype,
      $.hole,
      $.escape,
      $.html_if,
      $.html_for,
      $.html_switch,
      $.html_match,
      $.html_statement,
      $.template_header,
      alias($._html_text, $.text),
    ),

    element: $ => choice(
      seq(
        field('open', $.start_tag),
        repeat($._node),
        field('close', $.end_tag),
      ),
      $.self_closing_tag,
      $.void_tag,
    ),

    start_tag: $ => seq(
      '<',
      optional(seq(
        field('name', $.tag_name),
        optional(field('type_arguments', $.type_arguments)),
        repeat($._html_attribute),
      )),
      '>',
    ),

    self_closing_tag: $ => seq(
      '<',
      field('name', choice($.tag_name, alias($._void_tag_name, $.tag_name))),
      optional(field('type_arguments', $.type_arguments)),
      repeat($._html_attribute),
      '/>',
    ),

    void_tag: $ => seq(
      '<',
      field('name', alias($._void_tag_name, $.tag_name)),
      repeat($._html_attribute),
      '>',
    ),

    _void_tag_name: _ => choice(...voidElements),

    end_tag: $ => seq(
      '</',
      optional(seq(
        field('name', $.tag_name),
        optional(field('type_arguments', $.type_arguments)),
      )),
      '>',
    ),

    raw_element: $ => seq(
      '<',
      field('name', alias($._raw_tag_name, $.tag_name)),
      repeat($._html_attribute),
      '>',
      optional(field('content', $.raw_text)),
      '</',
      field('close_name', alias($._raw_tag_name, $.tag_name)),
      '>',
    ),

    _html_attribute: $ => choice($.html_attribute, $.hole),

    html_attribute: $ => seq(
      field('name', alias($.markup_attribute_name, $.attribute_name)),
      optional(seq('=', field('value', choice($.quoted_value, $.hole, $.unquoted_value)))),
    ),

    quoted_value: $ => choice(
      seq('"', repeat(choice(alias(token.immediate(prec(1, /[^"{]+/)), $.value_text), $.hole, $.escape)), '"'),
      seq('\'', repeat(choice(alias(token.immediate(prec(1, /[^'{]+/)), $.value_text), $.hole, $.escape)), '\''),
    ),

    unquoted_value: _ => /[^\s"'=<>`{}/]+/,

    html_comment: _ => token(seq('<!--', /[^-]*-+([^->][^-]*-+)*/, '>')),

    doctype: _ => token(seq('<!', /[dD][oO][cC][tT][yY][pP][eE]/, /[^>]*/, '>')),

    escape: _ => '{{',

    hole: $ => seq('{', optional($._markup_hole), '}'),

    html_body: $ => seq('{', repeat($._node), alias($._close_brace, '}')),

    html_if: $ => seq(
      'if',
      optional(seq(field('initializer', $._simple_statement), ';')),
      field('condition', $._expression),
      field('consequence', $.html_body),
      optional(seq(
        'else',
        field('alternative', choice($.html_body, $.html_if)),
      )),
    ),

    html_for: $ => seq(
      'for',
      optional(choice($._expression, $.for_clause, $.range_clause)),
      field('body', $.html_body),
    ),

    html_switch: $ => seq(
      'switch',
      optional(seq(field('initializer', $._simple_statement), ';')),
      field('value', optional($._expression)),
      '{',
      repeat($.html_case),
      alias($._close_brace, '}'),
    ),

    html_match: $ => seq(
      'match',
      field('value', $._expression),
      '{',
      repeat($.html_case),
      alias($._close_brace, '}'),
    ),

    html_case: $ => seq(
      choice(
        seq(
          'case',
          field('pattern', $.expression_list),
          optional(seq('if', field('guard', $._expression))),
        ),
        'default',
      ),
      ':',
      repeat($._node),
    ),

    html_statement: $ => seq(
      field('left', commaSep1($.identifier)),
      field('operator', choice(':=', '=')),
      field('right', $.expression_list),
      '\n',
    ),

    template_header: $ => seq(
      'func',
      optional(field('receiver', $.parameter_list)),
      field('name', $.identifier),
      optional(field('type_parameters', $.type_parameter_list)),
      field('parameters', $.parameter_list),
      '\n',
    ),
  },
});

function commaSep1(rule) {
  return seq(rule, repeat(seq(',', rule)));
}
