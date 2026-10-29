package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"runtime"
	"sort"
	"strings"
)

// A snapshot is the export data of the packages a playground project may
// import, prepared by the go command where the docs are built: the
// compiler's __.PKGDEF of each, which go/importer reads as it reads the
// build cache's archives. Export data is the compiler's own, so a snapshot
// is read only by a playground built with the same Go.

const snapshotMagic = "vuka-playground-types\n"

// encodeSnapshot is pkgs (import path → export data) as a snapshot file.
func encodeSnapshot(pkgs map[string][]byte) []byte {
	var b bytes.Buffer
	z, _ := gzip.NewWriterLevel(&b, gzip.BestCompression)
	w := bufio.NewWriter(z)
	w.WriteString(snapshotMagic)
	putString(w, runtime.Version())
	paths := make([]string, 0, len(pkgs))
	for p := range pkgs {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	putUvarint(w, uint64(len(paths)))
	for _, p := range paths {
		putString(w, p)
		putString(w, string(pkgs[p]))
	}
	w.Flush()
	z.Close()
	return b.Bytes()
}

// decodeSnapshot reads a snapshot file.
func decodeSnapshot(data []byte) (map[string][]byte, error) {
	z, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("types snapshot: %v", err)
	}
	r := bufio.NewReader(z)
	magic := make([]byte, len(snapshotMagic))
	if _, err := io.ReadFull(r, magic); err != nil || string(magic) != snapshotMagic {
		return nil, errors.New("types snapshot: not a snapshot")
	}
	goVersion, err := getString(r)
	if err != nil {
		return nil, fmt.Errorf("types snapshot: %v", err)
	}
	if goVersion != runtime.Version() {
		return nil, fmt.Errorf("types snapshot: made by %s, read by %s; build both with one Go", goVersion, runtime.Version())
	}
	n, err := binary.ReadUvarint(r)
	if err != nil {
		return nil, fmt.Errorf("types snapshot: %v", err)
	}
	pkgs := make(map[string][]byte, n)
	for range n {
		path, err := getString(r)
		if err != nil {
			return nil, fmt.Errorf("types snapshot: %v", err)
		}
		data, err := getString(r)
		if err != nil {
			return nil, fmt.Errorf("types snapshot: %s: %v", path, err)
		}
		pkgs[path] = []byte(data)
	}
	return pkgs, nil
}

// pkgdef is the __.PKGDEF member of a compiled package's archive, as an
// archive of that member alone.
func pkgdef(archive []byte) ([]byte, error) {
	const magic, header = "!<arch>\n", 60
	if !bytes.HasPrefix(archive, []byte(magic)) || len(archive) < len(magic)+header {
		return nil, errors.New("not an archive")
	}
	h := archive[len(magic) : len(magic)+header]
	if strings.TrimSpace(string(h[:16])) != "__.PKGDEF" {
		return nil, errors.New("no __.PKGDEF")
	}
	var size int
	if _, err := fmt.Sscan(strings.TrimSpace(string(h[48:58])), &size); err != nil {
		return nil, fmt.Errorf("member size: %v", err)
	}
	end := len(magic) + header + size
	if end > len(archive) {
		return nil, errors.New("short archive")
	}
	return archive[:end], nil
}

func putUvarint(w *bufio.Writer, n uint64) {
	var buf [binary.MaxVarintLen64]byte
	w.Write(buf[:binary.PutUvarint(buf[:], n)])
}

func putString(w *bufio.Writer, s string) {
	putUvarint(w, uint64(len(s)))
	w.WriteString(s)
}

func getString(r *bufio.Reader) (string, error) {
	n, err := binary.ReadUvarint(r)
	if err != nil {
		return "", err
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		return "", err
	}
	return string(b), nil
}
