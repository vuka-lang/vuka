package main

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/andybalholm/brotli"
)

// vuka build precompresses what the app serves as it is: the stylesheet,
// the components' modules and the island chunks embedded in the binary are
// registered with their brotli and gzip encodings (web.RegisterEncoded), and
// the island chunks build-islands writes into a frontend's build get a .br
// and a .gz beside them. The app sends a browser the encoding it takes, with
// no work at run time.

const (
	precompressMin = 1024 // smaller assets are sent as they are
	webPrefix      = "/_vuka/"
)

// encoded is an asset's encodings: nil when one isn't smaller.
type encoded struct{ br, gz []byte }

// precompress encodes data, a file called name: brotli at its best
// quality, gzip at level 9. A file under precompressMin, or of a type that
// doesn't gain by it (images, woff2, archives), gets none.
func precompress(name string, data []byte) encoded {
	if len(data) < precompressMin || !precompressible(name) {
		return encoded{}
	}
	var e encoded
	var b bytes.Buffer
	bw := brotli.NewWriterLevel(&b, brotli.BestCompression)
	bw.Write(data)
	bw.Close()
	if b.Len() < len(data) {
		e.br = bytes.Clone(b.Bytes())
	}
	b.Reset()
	gw, _ := gzip.NewWriterLevel(&b, gzip.BestCompression)
	gw.Write(data)
	gw.Close()
	if b.Len() < len(data) {
		e.gz = bytes.Clone(b.Bytes())
	}
	return e
}

func precompressible(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".js", ".mjs", ".css", ".json", ".map", ".svg", ".txt", ".xml", ".wasm", ".ttf", ".otf", ".ico", ".html":
		return true
	}
	return false
}

// writeEncoded writes file's encodings beside it (file.br, file.gz) and
// removes those it no longer has; it returns the names written.
func writeEncoded(file string, data []byte) ([]string, error) {
	e := precompress(file, data)
	var names []string
	for _, v := range []struct {
		ext string
		b   []byte
	}{{".br", e.br}, {".gz", e.gz}} {
		if v.b == nil {
			os.Remove(file + v.ext)
			continue
		}
		if err := writeIfChanged(file+v.ext, v.b); err != nil {
			return names, err
		}
		names = append(names, filepath.Base(file)+v.ext)
	}
	return names, nil
}

// encodedGo is the Go registering an asset's encodings with web, served at
// urlPath, its constants named after name: the init lines and the
// constants.
func encodedGo(urlPath, name string, data []byte) (calls, consts string) {
	e := precompress(urlPath, data)
	var c, k strings.Builder
	for _, v := range []struct {
		enc, suffix string
		b           []byte
	}{{"br", "Br", e.br}, {"gzip", "Gz", e.gz}} {
		if v.b == nil {
			continue
		}
		fmt.Fprintf(&c, "\tvukaweb.RegisterEncoded(%q, %q, []byte(%s%s))\n", urlPath, v.enc, name, v.suffix)
		fmt.Fprintf(&k, "\nconst %s%s = %s\n", name, v.suffix, strconv.Quote(string(v.b)))
	}
	return c.String(), k.String()
}
