package main

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

// wsConn is a WebSocket client connection, the little of RFC 6455 a test
// talking to a live page needs: text messages, pings answered, no
// extensions.
type wsConn struct {
	c net.Conn
	r *bufio.Reader
}

func dialWS(rawURL string) (*wsConn, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	c, err := net.DialTimeout("tcp", u.Host, 5*time.Second)
	if err != nil {
		return nil, err
	}
	key := make([]byte, 16)
	rand.Read(key)
	fmt.Fprintf(c, "GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n",
		u.RequestURI(), u.Host, base64.StdEncoding.EncodeToString(key))
	r := bufio.NewReader(c)
	res, err := http.ReadResponse(r, nil)
	if err != nil {
		c.Close()
		return nil, err
	}
	if res.StatusCode != http.StatusSwitchingProtocols {
		body, _ := io.ReadAll(res.Body)
		c.Close()
		return nil, fmt.Errorf("websocket upgrade: %s %s", res.Status, body)
	}
	return &wsConn{c: c, r: r}, nil
}

func (w *wsConn) Close() error { return w.c.Close() }

func (w *wsConn) frame(op byte, payload []byte) error {
	hdr := []byte{0x80 | op}
	switch n := len(payload); {
	case n < 126:
		hdr = append(hdr, 0x80|byte(n))
	case n < 1<<16:
		hdr = append(hdr, 0x80|126, byte(n>>8), byte(n))
	default:
		hdr = binary.BigEndian.AppendUint64(append(hdr, 0x80|127), uint64(n))
	}
	mask := make([]byte, 4)
	rand.Read(mask)
	hdr = append(hdr, mask...)
	masked := make([]byte, len(payload))
	for i, b := range payload {
		masked[i] = b ^ mask[i%4]
	}
	_, err := w.c.Write(append(hdr, masked...))
	return err
}

// Send sends a text message.
func (w *wsConn) Send(msg []byte) error { return w.frame(1, msg) }

// Read is the next text message, waiting at most d.
func (w *wsConn) Read(d time.Duration) ([]byte, error) {
	w.c.SetReadDeadline(time.Now().Add(d))
	var msg []byte
	for {
		var h [2]byte
		if _, err := io.ReadFull(w.r, h[:]); err != nil {
			return nil, err
		}
		fin, op := h[0]&0x80 != 0, h[0]&0x0f
		n := uint64(h[1] & 0x7f)
		switch n {
		case 126:
			var b [2]byte
			if _, err := io.ReadFull(w.r, b[:]); err != nil {
				return nil, err
			}
			n = uint64(binary.BigEndian.Uint16(b[:]))
		case 127:
			var b [8]byte
			if _, err := io.ReadFull(w.r, b[:]); err != nil {
				return nil, err
			}
			n = binary.BigEndian.Uint64(b[:])
		}
		payload := make([]byte, n)
		if _, err := io.ReadFull(w.r, payload); err != nil {
			return nil, err
		}
		switch op {
		case 8:
			return nil, fmt.Errorf("websocket closed: %q", payload)
		case 9:
			if err := w.frame(10, payload); err != nil {
				return nil, err
			}
			continue
		case 10:
			continue
		case 0, 1:
			msg = append(msg, payload...)
		default:
			return nil, errors.New("websocket: unexpected frame")
		}
		if fin {
			return msg, nil
		}
	}
}
