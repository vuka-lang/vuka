package main

import (
	"strconv"
	"strings"
)

// routeCall is a link by route name in a source, found by its text:
// route("users.edit", id), routePath("users"), or in Go web.Route(ctx,
// "users"), web.RouteURL and web.RoutePath. It serves the language server
// as it types and the build's diagnostics their places.
type routeCall struct {
	fn         string // route, routePath, web.Route, web.RouteURL, web.RoutePath
	start      int    // the call's first byte
	lparen     int
	end        int  // past its ), or len(src) when it isn't closed
	closed     bool // its ) is written
	name       string
	nameStart  int        // the name literal's opening quote; -1 when the call has none yet
	nameEnd    int        // past its closing quote (or where it stops, unclosed)
	nameClosed bool       // the literal is closed
	args       []argSpan  // its arguments after the name, a query literal among them
	query      *querySpan // its query{…} literal
}

// argSpan is an argument's text.
type argSpan struct{ start, end int }

// querySpan is a query{…} literal and its keys.
type querySpan struct {
	start, lbrace, end int
	keys               []queryKey
}

// queryKey is a literal key of a query literal.
type queryKey struct {
	key        string
	start, end int // the literal's quotes
}

var routeFns = []string{"web.RoutePath", "web.RouteURL", "web.Route", "routePath", "route"}

// findRouteCalls finds the route links of a Go source, or of the Go of a
// template's markup and script.
func findRouteCalls(src []byte) []routeCall {
	var out []routeCall
	s := string(src)
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '"' || c == '`':
			i = skipLit(s, i) - 1
			continue
		case c == '\'':
			i = skipRune(s, i)
			continue
		case c == '/' && i+1 < len(s) && s[i+1] == '/':
			if j := strings.IndexByte(s[i:], '\n'); j >= 0 {
				i += j
			} else {
				i = len(s)
			}
			continue
		case c != 'r' && c != 'w':
			continue
		}
		if i > 0 && (isIdentByte(s[i-1]) || s[i-1] == '.') {
			continue
		}
		for _, fn := range routeFns {
			if !strings.HasPrefix(s[i:], fn) {
				continue
			}
			j := i + len(fn)
			if j < len(s) && isIdentByte(s[j]) {
				continue
			}
			k := j
			for k < len(s) && (s[k] == ' ' || s[k] == '\t') {
				k++
			}
			if k >= len(s) || s[k] != '(' {
				continue
			}
			if rc, ok := parseRouteCall(s, i, k, fn); ok {
				out = append(out, rc)
			}
			break
		}
	}
	return out
}

// parseRouteCall reads the call of fn at start, its ( at lparen.
func parseRouteCall(s string, start, lparen int, fn string) (routeCall, bool) {
	rc := routeCall{fn: fn, start: start, lparen: lparen, nameStart: -1, end: len(s)}
	args := splitCallArgs(s, lparen+1)
	if len(args.spans) > 0 && args.closed {
		rc.end, rc.closed = args.end, true
	} else if args.closed {
		rc.end, rc.closed = args.end, true
	}
	first := 0
	if strings.HasPrefix(fn, "web.") {
		first = 1 // the ctx
	}
	if len(args.spans) <= first {
		return rc, true
	}
	ns := args.spans[first]
	at := ns.start
	for at < ns.end && (s[at] == ' ' || s[at] == '\t' || s[at] == '\n' || s[at] == '\r') {
		at++
	}
	if at < len(s) && (s[at] == '"' || s[at] == '`') {
		end := skipLit(s, at)
		rc.nameStart, rc.nameEnd = at, end
		rc.nameClosed = end <= len(s) && end-1 > at && s[end-1] == s[at]
		if rc.nameClosed {
			rc.name, _ = strconv.Unquote(s[at:end])
		} else {
			rc.name = s[at+1 : min(end, len(s))]
		}
	}
	for _, a := range args.spans[first+1:] {
		rc.args = append(rc.args, a)
		text := strings.TrimSpace(s[a.start:a.end])
		for _, q := range []string{"query{", "web.RouteQuery{"} {
			if strings.HasPrefix(text, q) {
				qs := a.start + strings.Index(s[a.start:a.end], q)
				rc.query = parseQuery(s, qs, qs+len(q)-1)
			}
		}
	}
	return rc, true
}

type argList struct {
	spans  []argSpan
	end    int
	closed bool
}

// splitCallArgs splits the arguments from from to the ) closing them.
func splitCallArgs(s string, from int) argList {
	var l argList
	depth, start := 0, from
	for i := from; i < len(s); i++ {
		switch s[i] {
		case '"', '`':
			i = skipLit(s, i) - 1
		case '\'':
			i = skipRune(s, i)
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth == 0 {
				if strings.TrimSpace(s[start:i]) != "" || len(l.spans) > 0 {
					l.spans = append(l.spans, argSpan{start, i})
				}
				l.end, l.closed = i+1, true
				return l
			}
			depth--
		case ',':
			if depth == 0 {
				l.spans = append(l.spans, argSpan{start, i})
				start = i + 1
			}
		case '\n':
			if depth == 0 && strings.TrimSpace(s[start:i]) == "" && len(l.spans) == 0 && i > from+200 {
				return l
			}
		}
	}
	l.spans = append(l.spans, argSpan{start, len(s)})
	l.end = len(s)
	return l
}

// parseQuery reads a query literal, its { at lbrace.
func parseQuery(s string, start, lbrace int) *querySpan {
	q := &querySpan{start: start, lbrace: lbrace, end: len(s)}
	depth := 0
	expectKey := true
	for i := lbrace + 1; i < len(s); i++ {
		switch c := s[i]; c {
		case '"', '`':
			end := skipLit(s, i)
			if depth == 0 && expectKey {
				k, _ := strconv.Unquote(s[i:min(end, len(s))])
				rest := strings.TrimLeft(s[min(end, len(s)):], " \t")
				if strings.HasPrefix(rest, ":") || rest == "" {
					q.keys = append(q.keys, queryKey{key: k, start: i, end: end})
				}
			}
			expectKey = false
			i = end - 1
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth == 0 {
				q.end = i + 1
				return q
			}
			depth--
		case ',':
			if depth == 0 {
				expectKey = true
			}
		case ' ', '\t', '\n', '\r':
		default:
			if depth == 0 {
				expectKey = false
			}
		}
	}
	return q
}

// skipRune is the last byte of the rune literal at i, i when it is none: an
// apostrophe in a template's text.
func skipRune(s string, i int) int {
	for j := i + 2; j < len(s) && j <= i+5; j++ {
		if s[j] == '\'' && (s[j-1] != '\\' || j == i+3) {
			return j
		}
	}
	return i
}

// skipLit is past the string, raw string or rune literal at i; len(s)
// when it isn't closed.
func skipLit(s string, i int) int {
	q := s[i]
	for j := i + 1; j < len(s); j++ {
		switch s[j] {
		case '\\':
			if q != '`' {
				j++
			}
		case '\n':
			if q != '`' {
				return j
			}
		case q:
			return j + 1
		}
	}
	return len(s)
}

// argAt is the index of the argument after the name off is in, -1 in the
// name or before it.
func (rc routeCall) argAt(off int) int {
	for i, a := range rc.args {
		if off >= a.start && off <= a.end {
			return i
		}
	}
	return -1
}

// inName reports whether off is inside the name literal.
func (rc routeCall) inName(off int) bool {
	return rc.nameStart >= 0 && off > rc.nameStart && (off < rc.nameEnd || !rc.nameClosed && off <= rc.nameEnd)
}

// pathArgs is how many arguments it gives the path, its query left out.
func (rc routeCall) pathArgs(src []byte) int {
	n := 0
	for _, a := range rc.args {
		if t := strings.TrimSpace(string(src[a.start:a.end])); t != "" && (rc.query == nil || a.start > rc.query.start || a.end < rc.query.start) {
			n++
		}
	}
	return n
}
