package main

import (
	"bytes"
	"io"
	"mime"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

// Path mode serves every program at /s/<id>/ on one origin, while a
// program writes root-absolute URLs (/_vuka/app.css, /pets). The proxy
// rewrites those in the HTML and CSS it passes on, and each page loads
// shimPath first: it puts the prefix on the URLs the page's scripts build
// (fetch, XMLHttpRequest, WebSocket, EventSource, history) and on links
// and forms a live patch brought, and takes it off the URLs a vuka live
// socket's navigate messages carry. Nothing depends on cookies or the
// Referer.

const shimPath = "/_vkrun/path.js"

var (
	attrURL   = regexp.MustCompile(`(?i)(\s(?:href|src|action|formaction|poster|cite|background)\s*=\s*["']?)(/(?:[^/"'\s>][^"'\s>]*)?)(["'\s>])`)
	srcsetRE  = regexp.MustCompile(`(?i)(\ssrcset\s*=\s*)(["'])([^"']*)(["'])`)
	cssURL    = regexp.MustCompile(`(?i)(url\(\s*["']?)(/(?:[^/"')\s][^"')\s]*)?)(["')\s])`)
	cssImport = regexp.MustCompile(`(?i)(@import\s+["'])(/(?:[^/"'][^"']*)?)(["'])`)
	headOpen  = regexp.MustCompile(`(?i)<head(\s[^>]*)?>`)
)

// prefixURL puts prefix on a root-absolute URL that hasn't it yet.
func prefixURL(prefix, u string) string {
	if u == prefix || strings.HasPrefix(u, prefix+"/") || strings.HasPrefix(u, prefix+"?") || u == shimPath {
		return u
	}
	return prefix + u
}

func rewriteCSS(prefix string, b []byte) []byte {
	fix := func(re *regexp.Regexp, b []byte) []byte {
		return re.ReplaceAllFunc(b, func(m []byte) []byte {
			sm := re.FindSubmatch(m)
			return append(append(append([]byte{}, sm[1]...), prefixURL(prefix, string(sm[2]))...), sm[3]...)
		})
	}
	return fix(cssImport, fix(cssURL, b))
}

func rewriteHTML(prefix string, b []byte) []byte {
	b = attrURL.ReplaceAllFunc(b, func(m []byte) []byte {
		sm := attrURL.FindSubmatch(m)
		return append(append(append([]byte{}, sm[1]...), prefixURL(prefix, string(sm[2]))...), sm[3]...)
	})
	b = srcsetRE.ReplaceAllFunc(b, func(m []byte) []byte {
		sm := srcsetRE.FindSubmatch(m)
		parts := strings.Split(string(sm[3]), ",")
		for i, p := range parts {
			t := strings.TrimLeft(p, " \t\n")
			if strings.HasPrefix(t, "/") && !strings.HasPrefix(t, "//") {
				parts[i] = p[:len(p)-len(t)] + prefixURL(prefix, t)
			}
		}
		return []byte(string(sm[1]) + string(sm[2]) + strings.Join(parts, ",") + string(sm[4]))
	})
	b = rewriteCSS(prefix, b)
	tag := `<script src="` + shimPath + `" data-prefix="` + prefix + `"></script>`
	if loc := headOpen.FindIndex(b); loc != nil {
		return append(append(append([]byte{}, b[:loc[1]]...), tag...), b[loc[1]:]...)
	}
	return append([]byte(tag), b...)
}

// rewriteBody rewrites a path-mode response's HTML or CSS for prefix.
func rewriteBody(resp *http.Response, prefix string) error {
	mt, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	var fn func(string, []byte) []byte
	switch mt {
	case "text/html", "application/xhtml+xml":
		fn = rewriteHTML
	case "text/css":
		fn = rewriteCSS
	default:
		return nil
	}
	if ce := resp.Header.Get("Content-Encoding"); ce != "" && ce != "identity" {
		return nil // asked for identity; a program that compresses anyway passes as is
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	resp.Body.Close()
	if err != nil {
		return err
	}
	b = fn(prefix, b)
	resp.Body = io.NopCloser(bytes.NewReader(b))
	resp.ContentLength = int64(len(b))
	resp.Header.Set("Content-Length", strconv.Itoa(len(b)))
	resp.Header.Del("Etag") // the bytes changed
	return nil
}

func serveShim(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	io.WriteString(w, shimJS)
}

const shimJS = `(function () {
  "use strict";
  var s = document.currentScript, p = s && s.getAttribute("data-prefix");
  if (!p || window.__vkrun) return;
  window.__vkrun = p;
  function fix(u) {
    if (u == null || (typeof u !== "string" && !(u instanceof URL))) return u;
    var str = String(u);
    if (str.charAt(0) === "/" && str.charAt(1) !== "/") {
      return str === p || str.indexOf(p + "/") === 0 || str.indexOf(p + "?") === 0 ? str : p + str;
    }
    var x;
    try { x = new URL(str, location.href); } catch (e) { return u; }
    if (x.host !== location.host || x.pathname === p || x.pathname.indexOf(p + "/") === 0) return u;
    x.pathname = p + x.pathname;
    return x.href;
  }
  var f = window.fetch;
  if (f) window.fetch = function (input, init) {
    if (input instanceof Request) { var to = fix(input.url); if (to !== input.url) input = new Request(to, input); }
    else input = fix(input);
    return f.call(this, input, init);
  };
  var open = XMLHttpRequest.prototype.open;
  XMLHttpRequest.prototype.open = function (m, u) { arguments[1] = fix(u); return open.apply(this, arguments); };
  function wrap(C, onMake) {
    if (!C) return C;
    function W(u, o) { var c = o === undefined ? new C(fix(u)) : new C(fix(u), o); if (onMake) onMake(c); return c; }
    W.prototype = C.prototype;
    for (var k in C) try { W[k] = C[k]; } catch (e) {}
    ["CONNECTING", "OPEN", "CLOSING", "CLOSED"].forEach(function (k) { if (k in C) Object.defineProperty(W, k, { value: C[k] }); });
    return W;
  }
  var nav = '"url":"' + p + "/";
  window.WebSocket = wrap(window.WebSocket, function (ws) {
    var send = ws.send;
    ws.send = function (d) { return send.call(ws, typeof d === "string" ? d.split(nav).join('"url":"/') : d); };
  });
  window.EventSource = wrap(window.EventSource);
  ["pushState", "replaceState"].forEach(function (k) {
    var h = history[k];
    history[k] = function (st, t, u) { return arguments.length > 2 ? h.call(history, st, t, fix(u)) : h.apply(history, arguments); };
  });
  function attr(el, a) {
    var v = el && el.getAttribute(a);
    if (v) { var to = fix(v); if (to !== v) el.setAttribute(a, to); }
  }
  window.addEventListener("click", function (e) {
    var a = e.target && e.target.closest && e.target.closest("a[href]");
    attr(a, "href");
    var b = e.target && e.target.closest && e.target.closest("[formaction]");
    attr(b, "formaction");
  }, true);
  window.addEventListener("submit", function (e) { attr(e.target, "action"); }, true);
  ["mouseover", "focusin", "touchstart"].forEach(function (t) {
    window.addEventListener(t, function (e) { attr(e.target && e.target.closest && e.target.closest("a[href]"), "href"); }, { capture: true, passive: true });
  });
})();
`
