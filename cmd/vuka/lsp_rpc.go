package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// rpcConn is one side of an LSP stream: JSON-RPC 2.0 messages framed by a
// Content-Length header.
type rpcConn struct {
	r   *bufio.Reader
	wmu sync.Mutex
	w   io.Writer
}

func newRPCConn(r io.Reader, w io.Writer) *rpcConn {
	return &rpcConn{r: bufio.NewReaderSize(r, 1<<16), w: w}
}

// read returns the next message body.
func (c *rpcConn) read() ([]byte, error) {
	n := -1
	for {
		line, err := c.r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if ok && strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			if n, err = strconv.Atoi(strings.TrimSpace(value)); err != nil {
				return nil, fmt.Errorf("lsp: bad Content-Length %q", value)
			}
		}
	}
	if n < 0 {
		return nil, fmt.Errorf("lsp: message without Content-Length")
	}
	body := make([]byte, n)
	_, err := io.ReadFull(c.r, body)
	return body, err
}

func (c *rpcConn) write(body []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if _, err := fmt.Fprintf(c.w, "Content-Length: %d\r\n\r\n", len(body)); err != nil {
		return err
	}
	_, err := c.w.Write(body)
	return err
}

// send marshals v and writes it.
func (c *rpcConn) send(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.write(b)
}

// rpcMsg is any JSON-RPC message: a request (ID + Method), a notification
// (Method), or a response (ID + Result or Error).
type rpcMsg struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   json.RawMessage `json:"error,omitempty"`
}

func (m *rpcMsg) isRequest() bool      { return m.Method != "" && len(m.ID) > 0 }
func (m *rpcMsg) isNotification() bool { return m.Method != "" && len(m.ID) == 0 }
func (m *rpcMsg) isResponse() bool     { return m.Method == "" && len(m.ID) > 0 }

// LSP shapes the proxy reads and writes.
type lspPosition struct {
	Line      uint32 `json:"line"`
	Character uint32 `json:"character"`
}

type lspRange struct {
	Start lspPosition `json:"start"`
	End   lspPosition `json:"end"`
}

type lspDiagnostic struct {
	Range    lspRange `json:"range"`
	Severity int      `json:"severity"`
	Source   string   `json:"source,omitempty"`
	Message  string   `json:"message"`
}

type textDocumentID struct {
	URI string `json:"uri"`
}

// uriToPath turns a file:// URI into a clean OS path ("" for other schemes).
func uriToPath(uri string) string {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return ""
	}
	return filepath.Clean(filepath.FromSlash(u.Path))
}

// pathToURI is uriToPath's inverse.
func pathToURI(path string) string {
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String()
}
