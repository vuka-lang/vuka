#include "tree_sitter/parser.h"

#include <stdbool.h>
#include <string.h>

enum TokenType {
  MARKUP_TEXT,
  BLOCK_TEXT,
  RAW_TEXT,
  HTML_TEXT,
  CLOSE_BRACE,
  ERROR_SENTINEL,
};

static bool is_space(int32_t c) { return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f'; }

static bool is_word(int32_t c) {
  return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_';
}

static bool ends_text(int32_t c) { return c == '<' || c == '{' || c == '}'; }

static bool is(const char *word, const char *keyword) { return strcmp(word, keyword) == 0; }

// starts_header reports whether word, followed by next, starts a nested if,
// for, else, match or case in a markup block's body rather than its text.
static bool starts_header(const char *word, int32_t next) {
  if (is(word, "if") || is(word, "for")) return is_space(next) || next == '(';
  if (is(word, "else")) return is_space(next) || next == '{';
  if (is(word, "match") || is(word, "case")) return is_space(next);
  if (is(word, "default")) return is_space(next) || next == ':';
  return false;
}

// starts_statement reports whether a line of a .vuka.html file starting
// with word, then next, is Go: a statement's header, a case, a template's
// func header or a declaration of the Go header.
static bool starts_statement(const char *word, int32_t next) {
  if (starts_header(word, next)) return true;
  if (is(word, "switch")) return is_space(next) || next == '{';
  if (is(word, "import") || is(word, "type") || is(word, "const") || is(word, "var")) {
    return is_space(next) || next == '(';
  }
  return (is(word, "package") || is(word, "func") || is(word, "enum") || is(word, "decorator")) && is_space(next);
}

// read_word reads the identifier at the lexer into word, keeping it as
// text so far; its length, 0 when there is none.
static unsigned read_word(TSLexer *lexer, char *word, unsigned size) {
  unsigned n = 0;
  while (is_word(lexer->lookahead) && n < size - 1) {
    word[n++] = (char)lexer->lookahead;
    lexer->advance(lexer, false);
  }
  word[n] = 0;
  if (n > 0) lexer->mark_end(lexer);
  return n;
}

// skip_quoted skips a string the lexer is at the quote of: a backquoted one
// to its closing quote, another to its line's end at most.
static void skip_quoted(TSLexer *lexer) {
  int32_t quote = lexer->lookahead;
  lexer->advance(lexer, false);
  while (!lexer->eof(lexer) && lexer->lookahead != quote) {
    if (quote != '`' && lexer->lookahead == '\n') return;
    if (quote != '`' && lexer->lookahead == '\\') lexer->advance(lexer, false);
    lexer->advance(lexer, false);
  }
  lexer->advance(lexer, false);
}

// scan_raw is a <script> or <style> element's content: everything up to the
// </script or </style closing it, outside a string.
static bool scan_raw(TSLexer *lexer) {
  lexer->result_symbol = RAW_TEXT;
  bool any = false, consumed = false;
  for (;;) {
    lexer->mark_end(lexer);
    any = consumed;
    if (lexer->eof(lexer)) return any;
    consumed = true;
    if (lexer->lookahead == '"' || lexer->lookahead == '\'' || lexer->lookahead == '`') {
      skip_quoted(lexer);
      continue;
    }
    if (lexer->lookahead != '<') {
      lexer->advance(lexer, false);
      continue;
    }
    lexer->advance(lexer, false);
    if (lexer->lookahead != '/') continue;
    lexer->advance(lexer, false);
    char name[8];
    unsigned n = 0;
    while (is_word(lexer->lookahead) && n < sizeof name - 1) {
      name[n++] = (char)lexer->lookahead;
      lexer->advance(lexer, false);
    }
    name[n] = 0;
    if (!is_word(lexer->lookahead) && (is(name, "script") || is(name, "style"))) return any;
  }
}

// scan_markup_text is a .vuka file's markup text: it runs to the next tag,
// hole or closing brace, without the whitespace around it. In a block's
// body it never starts with a keyword: that is a nested if, for, match or
// case.
static bool scan_markup_text(TSLexer *lexer, bool block) {
  while (is_space(lexer->lookahead)) lexer->advance(lexer, true);
  if (lexer->eof(lexer) || ends_text(lexer->lookahead)) return false;
  if (block) {
    char word[8];
    if (read_word(lexer, word, sizeof word) > 0 && !is_word(lexer->lookahead) &&
        starts_header(word, lexer->lookahead)) {
      return false;
    }
  }
  while (!lexer->eof(lexer) && !ends_text(lexer->lookahead)) {
    bool space = is_space(lexer->lookahead);
    lexer->advance(lexer, false);
    if (!space) lexer->mark_end(lexer);
  }
  lexer->result_symbol = block ? BLOCK_TEXT : MARKUP_TEXT;
  return true;
}

// assigns reports whether the rest of a line after its first name is an
// assignment's: more names, then = or :=. What it reads is text otherwise.
static bool assigns(TSLexer *lexer) {
  while (is_word(lexer->lookahead) || lexer->lookahead == '.' || lexer->lookahead == ',' ||
         lexer->lookahead == ' ' || lexer->lookahead == '\t') {
    bool space = lexer->lookahead == ' ' || lexer->lookahead == '\t';
    lexer->advance(lexer, false);
    if (!space) lexer->mark_end(lexer);
  }
  if (lexer->lookahead == ':') lexer->advance(lexer, false);
  if (lexer->lookahead != '=') return false;
  lexer->advance(lexer, false);
  return lexer->lookahead != '=';
}

// scan_html_text is a line of a .vuka.html file's text, up to the next tag
// or hole, or the closing brace of the block it is in. A line that is Go —
// a declaration, a statement's header, a case, an assignment, an attribute
// or a comment — is not text.
static bool scan_html_text(TSLexer *lexer, bool closes) {
  bool line_start = lexer->get_column(lexer) == 0;
  while (is_space(lexer->lookahead)) {
    if (lexer->lookahead == '\n') line_start = true;
    lexer->advance(lexer, true);
  }
  if (closes && lexer->lookahead == '}') {
    lexer->advance(lexer, false);
    lexer->mark_end(lexer);
    lexer->result_symbol = CLOSE_BRACE;
    return true;
  }
  if (lexer->eof(lexer) || lexer->lookahead == '<' || lexer->lookahead == '{') return false;
  if (line_start && lexer->lookahead == '@') return false;
  if (line_start && lexer->lookahead == '/') {
    lexer->advance(lexer, false);
    if (lexer->lookahead == '/' || lexer->lookahead == '*') return false;
    lexer->mark_end(lexer);
  }
  char word[10];
  if (read_word(lexer, word, sizeof word) > 0 && !is_word(lexer->lookahead)) {
    int32_t next = lexer->lookahead;
    if (is(word, "else") && (is_space(next) || next == '{')) return false;
    if (line_start && (starts_statement(word, next) || assigns(lexer))) return false;
  }
  while (!lexer->eof(lexer) && lexer->lookahead != '\n' && lexer->lookahead != '<' && lexer->lookahead != '{' &&
         !(closes && lexer->lookahead == '}')) {
    bool space = is_space(lexer->lookahead);
    lexer->advance(lexer, false);
    if (!space) lexer->mark_end(lexer);
  }
  lexer->result_symbol = HTML_TEXT;
  return true;
}

static bool scan(TSLexer *lexer, const bool *valid_symbols) {
  if (valid_symbols[ERROR_SENTINEL]) return false;
  if (valid_symbols[RAW_TEXT]) return scan_raw(lexer);
  if (valid_symbols[HTML_TEXT] || valid_symbols[CLOSE_BRACE]) {
    return scan_html_text(lexer, valid_symbols[CLOSE_BRACE]);
  }
  if (valid_symbols[BLOCK_TEXT] || valid_symbols[MARKUP_TEXT]) {
    return scan_markup_text(lexer, valid_symbols[BLOCK_TEXT]);
  }
  return false;
}

#define SCANNER(name)                                                                                       \
  void *tree_sitter_##name##_external_scanner_create(void) { return NULL; }                                 \
  void tree_sitter_##name##_external_scanner_destroy(void *payload) {}                                      \
  unsigned tree_sitter_##name##_external_scanner_serialize(void *payload, char *buffer) { return 0; }       \
  void tree_sitter_##name##_external_scanner_deserialize(void *payload, const char *buffer, unsigned n) {}  \
  bool tree_sitter_##name##_external_scanner_scan(void *payload, TSLexer *lexer, const bool *valid) {       \
    return scan(lexer, valid);                                                                              \
  }
