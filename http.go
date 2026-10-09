package vuka

import (
	"net/http"

	"github.com/a-h/templ"
)

// Handler serves the Node f builds for each request through templ.Handler:
// rendered in full with the request's context, then sent as text/html. An
// error from f or from rendering answers 500 with only the status text.
func Handler(f func(r *http.Request) (Node, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, err := f(r)
		if err != nil {
			serverError(w)
			return
		}
		if n == nil {
			n = Fragment()
		}
		templ.Handler(n, templ.WithErrorHandler(func(*http.Request, error) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { serverError(w) })
		})).ServeHTTP(w, r)
	})
}

func serverError(w http.ResponseWriter) {
	http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
}

// Write renders n with r's context and sends it as text/html (unless a
// Content-Type is set already). The page is rendered in full first, so a
// render error is returned with nothing written.
func Write(w http.ResponseWriter, r *http.Request, n Node) error {
	buf := templ.GetBuffer()
	defer templ.ReleaseBuffer(buf)
	if n != nil {
		if err := n.Render(r.Context(), buf); err != nil {
			return err
		}
	}
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	}
	_, err := w.Write(buf.Bytes())
	return err
}
