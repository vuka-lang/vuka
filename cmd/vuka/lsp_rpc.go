package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// rpcConn is one side of an LSP stream: JSON-RPC 2.0 messages framed by a
// Content-Length header.
type rpcConn struct {
	r   *bufio.Reader
	wmu sync.Mutex
	w   io.Writer

	mu              sync.Mutex
	onRead, onWrite func([]byte) []byte // rewrite message bodies, when set
	drop            func([]byte) bool   // reports whether a message is not to be written, when set

	// A queued conn's writes return at once; one goroutine writes them in
	// order (see queued).
	qmu     sync.Mutex
	qcond   *sync.Cond
	pending [][]byte
	writing bool
}

// queued makes c's writes return without waiting for the other side to
// read them: gopls, busy loading a workspace, stops reading for minutes,
// and the proxy must still answer the editor meanwhile.
func (c *rpcConn) queued() *rpcConn {
	c.qcond = sync.NewCond(&c.qmu)
	go func() {
		c.qmu.Lock()
		for {
			for len(c.pending) == 0 {
				c.qcond.Wait()
			}
			body := c.pending[0]
			c.pending = c.pending[1:]
			c.writing = true
			c.qmu.Unlock()
			err := c.writeNow(body)
			c.qmu.Lock()
			c.writing = false
			c.qcond.Broadcast()
			if err != nil {
				c.pending = nil
				c.qcond = nil // writes from now on are dropped
				c.qmu.Unlock()
				return
			}
		}
	}()
	return c
}

// flush waits, at most d, until a queued conn has written what it was
// given.
func (c *rpcConn) flush(d time.Duration) {
	c.qmu.Lock()
	defer c.qmu.Unlock()
	if c.qcond == nil {
		return
	}
	cond := c.qcond
	expired := false
	t := time.AfterFunc(d, func() {
		c.qmu.Lock()
		expired = true
		cond.Broadcast()
		c.qmu.Unlock()
	})
	defer t.Stop()
	for (len(c.pending) > 0 || c.writing) && !expired && c.qcond != nil {
		cond.Wait()
	}
}

// rewriting installs body rewrites for messages read and written.
func (c *rpcConn) rewriting(onRead, onWrite func([]byte) []byte) {
	c.mu.Lock()
	c.onRead, c.onWrite = onRead, onWrite
	c.mu.Unlock()
}

// dropping installs a check of the messages written: those it reports are
// not written.
func (c *rpcConn) dropping(f func([]byte) bool) {
	c.mu.Lock()
	c.drop = f
	c.mu.Unlock()
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
	c.mu.Lock()
	f := c.onRead
	c.mu.Unlock()
	if f != nil && err == nil {
		body = f(body)
	}
	return body, err
}

func (c *rpcConn) write(body []byte) error {
	c.mu.Lock()
	drop := c.drop
	c.mu.Unlock()
	if drop != nil && drop(body) {
		return nil
	}
	return c.writeChecked(body)
}

// writeChecked writes body past the drop check.
func (c *rpcConn) writeChecked(body []byte) error {
	c.mu.Lock()
	f := c.onWrite
	c.mu.Unlock()
	if f != nil {
		body = f(body)
	}
	c.qmu.Lock()
	if c.qcond != nil {
		c.pending = append(c.pending, body)
		c.qcond.Signal()
		c.qmu.Unlock()
		return nil
	}
	c.qmu.Unlock()
	return c.writeNow(body)
}

func (c *rpcConn) writeNow(body []byte) error {
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
	Range           lspRange     `json:"range"`
	Severity        int          `json:"severity"`
	Code            string       `json:"code,omitempty"`
	CodeDescription *lspCodeDesc `json:"codeDescription,omitempty"`
	Source          string       `json:"source,omitempty"`
	Message         string       `json:"message"`
	// RelatedInformation is the places the message names: the errors of a
	// package an import fails for.
	RelatedInformation []lspRelated `json:"relatedInformation,omitempty"`
}

type lspRelated struct {
	Location lspLocation `json:"location"`
	Message  string      `json:"message"`
}

type lspLocation struct {
	URI   string   `json:"uri"`
	Range lspRange `json:"range"`
}

type lspCodeDesc struct {
	Href string `json:"href"`
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
	return uriOSPath(runtime.GOOS, u.Path)
}

// pathToURI is uriToPath's inverse.
func pathToURI(path string) string {
	return (&url.URL{Scheme: "file", Path: fileURIPath(runtime.GOOS, path)}).String()
}
