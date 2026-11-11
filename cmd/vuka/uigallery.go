package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/vuka-lang/ui/v2/gallery"
)

// vuka ui gallery: every component of the kit in every state, light and
// dark, served or written to a directory (package gallery of the kit).

const galleryAddr = "localhost:7357"

// uiGallery is vuka ui gallery.
func uiGallery(f *uiFlags, stdout io.Writer) error {
	if len(f.pos) > 0 {
		return fmt.Errorf("vuka ui gallery takes no arguments (%s); usage: vuka ui gallery [--addr host:port] [--out dir] [--css file]…", strings.Join(f.pos, " "))
	}
	if f.out != "" && f.addr != "" {
		return errors.New("vuka ui gallery: --out writes the pages, --addr serves them: give one")
	}
	var opts gallery.Options
	// The kit's pages name vuka ui add for the components it writes a
	// copy of, from the registry built into vuka.
	if reg, err := readRegistry(regSource{}); err == nil {
		opts.Vendored = map[string]bool{}
		for _, name := range reg.components() {
			opts.Vendored[name] = true
		}
	}
	for _, css := range f.cssFiles {
		abs, err := filepath.Abs(css)
		if err != nil {
			return err
		}
		if fi, err := os.Stat(abs); err != nil {
			return fmt.Errorf("vuka ui gallery: --css %s: %w", css, err)
		} else if fi.IsDir() {
			return fmt.Errorf("vuka ui gallery: --css %s is a directory; give the stylesheet the browser loads (the built CSS)", css)
		}
		opts.CSSFiles = append(opts.CSSFiles, abs)
	}
	if f.out != "" {
		if err := gallery.Write(f.out, opts); err != nil {
			return fmt.Errorf("vuka ui gallery: %w", err)
		}
		fmt.Fprintf(stdout, "vuka ui gallery: wrote %s (index.html, a page per component in components/, states.html, light.html, dark.html)\n", f.out)
		return nil
	}
	addr := f.addr
	if addr == "" {
		addr = galleryAddr
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("vuka ui gallery: %w", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return serveGallery(ctx, ln, opts, stdout)
}

// serveGallery serves the gallery on ln until ctx is done.
func serveGallery(ctx context.Context, ln net.Listener, opts gallery.Options, stdout io.Writer) error {
	fmt.Fprintf(stdout, "vuka ui gallery: %s (every state: /states.html; Ctrl-C stops)\n", galleryURL(ln.Addr()))
	srv := &http.Server{Handler: gallery.Handler(opts), ReadHeaderTimeout: 10 * time.Second}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ln) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		shut, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return srv.Shutdown(shut)
	}
}

// galleryURL is the URL a browser opens for a listener's address.
func galleryURL(a net.Addr) string {
	host, port, err := net.SplitHostPort(a.String())
	if err != nil {
		return "http://" + a.String() + "/"
	}
	if ip := net.ParseIP(host); ip == nil || ip.IsUnspecified() || ip.IsLoopback() {
		host = "localhost"
	}
	return "http://" + net.JoinHostPort(host, port) + "/"
}
