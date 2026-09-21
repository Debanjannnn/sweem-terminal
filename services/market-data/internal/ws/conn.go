// Package ws holds a minimal RFC 6455 codec and the browser fan-out hub.
package ws

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	opContinuation = 0x0
	OpText         = 0x1
	OpBinary       = 0x2
	opClose        = 0x8
	opPing         = 0x9
	opPong         = 0xA

	// ponytail: 8 MiB frame ceiling, raise if a venue snapshot ever exceeds it
	maxMessage = 8 << 20
	magicGUID  = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
)

type CloseError struct {
	Code   int
	Reason string
}

func (e *CloseError) Error() string { return fmt.Sprintf("websocket closed %d: %s", e.Code, e.Reason) }

// CloseCode returns the peer's close code, or 1006 for any other failure.
func CloseCode(err error) int {
	var ce *CloseError
	if errors.As(err, &ce) {
		return ce.Code
	}
	return 1006
}

type Conn struct {
	c    net.Conn
	br   *bufio.Reader
	mask bool // clients mask, servers must not
	wmu  sync.Mutex
}

func (c *Conn) NetConn() net.Conn { return c.c }

func (c *Conn) Close() error { return c.c.Close() }

// Accept upgrades an inbound HTTP request. Origin is not checked: this plane is
// public read-only data.
func Accept(w http.ResponseWriter, r *http.Request) (*Conn, error) {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return nil, errors.New("request is not a websocket upgrade")
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		return nil, errors.New("request is missing Sec-WebSocket-Key")
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		return nil, errors.New("this server cannot hijack the connection")
	}
	nc, rw, err := hj.Hijack()
	if err != nil {
		return nil, err
	}
	resp := "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: " + acceptKey(key) + "\r\n\r\n"
	if _, err := rw.Writer.WriteString(resp); err != nil {
		nc.Close()
		return nil, err
	}
	if err := rw.Writer.Flush(); err != nil {
		nc.Close()
		return nil, err
	}
	return &Conn{c: nc, br: rw.Reader}, nil
}

// Dial opens a client connection to a ws:// or wss:// URL.
func Dial(rawURL string, timeout time.Duration) (*Conn, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	host := u.Host
	var nc net.Conn
	switch u.Scheme {
	case "wss":
		if u.Port() == "" {
			host = net.JoinHostPort(host, "443")
		}
		nc, err = tls.DialWithDialer(&net.Dialer{Timeout: timeout}, "tcp", host, &tls.Config{ServerName: u.Hostname()})
	case "ws":
		if u.Port() == "" {
			host = net.JoinHostPort(host, "80")
		}
		nc, err = net.DialTimeout("tcp", host, timeout)
	default:
		return nil, fmt.Errorf("%q is not a websocket url", rawURL)
	}
	if err != nil {
		return nil, err
	}
	var nonce [16]byte
	rand.Read(nonce[:])
	key := base64.StdEncoding.EncodeToString(nonce[:])
	path := u.RequestURI()
	req := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n",
		path, u.Host, key)
	nc.SetDeadline(time.Now().Add(timeout))
	if _, err := io.WriteString(nc, req); err != nil {
		nc.Close()
		return nil, err
	}
	br := bufio.NewReaderSize(nc, 64<<10)
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodGet})
	if err != nil {
		nc.Close()
		return nil, err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		nc.Close()
		return nil, fmt.Errorf("%s refused the upgrade with HTTP %d", rawURL, resp.StatusCode)
	}
	if resp.Header.Get("Sec-WebSocket-Accept") != acceptKey(key) {
		nc.Close()
		return nil, errors.New("server returned a Sec-WebSocket-Accept that does not match the key we sent")
	}
	nc.SetDeadline(time.Time{})
	return &Conn{c: nc, br: br, mask: true}, nil
}

func acceptKey(key string) string {
	sum := sha1.Sum([]byte(key + magicGUID))
	return base64.StdEncoding.EncodeToString(sum[:])
}

// Read returns the next data message, answering pings and surfacing close
// frames as *CloseError. Fragments are reassembled.
func (c *Conn) Read() (opcode byte, payload []byte, err error) {
	var buf []byte
	dataOp := byte(0)
	for {
		fin, op, frame, err := c.readFrame()
		if err != nil {
			return 0, nil, err
		}
		switch op {
		case opPing:
			if err := c.write(opPong, frame); err != nil {
				return 0, nil, err
			}
			continue
		case opPong:
			continue
		case opClose:
			code, reason := 1005, ""
			if len(frame) >= 2 {
				code, reason = int(binary.BigEndian.Uint16(frame[:2])), string(frame[2:])
			}
			c.write(opClose, frame)
			c.c.Close()
			return 0, nil, &CloseError{Code: code, Reason: reason}
		case opContinuation:
			if dataOp == 0 {
				return 0, nil, errors.New("continuation frame arrived with no message open")
			}
		case OpText, OpBinary:
			if dataOp != 0 {
				return 0, nil, errors.New("a new data frame arrived while a fragmented message was open")
			}
			dataOp = op
		default:
			return 0, nil, fmt.Errorf("opcode %d is not one this codec speaks", op)
		}
		buf = append(buf, frame...)
		if len(buf) > maxMessage {
			return 0, nil, errors.New("message exceeds the frame ceiling")
		}
		if fin {
			return dataOp, buf, nil
		}
	}
}

func (c *Conn) readFrame() (fin bool, op byte, payload []byte, err error) {
	var h [2]byte
	if _, err := io.ReadFull(c.br, h[:]); err != nil {
		return false, 0, nil, err
	}
	fin = h[0]&0x80 != 0
	op = h[0] & 0x0F
	masked := h[1]&0x80 != 0
	n := uint64(h[1] & 0x7F)
	switch n {
	case 126:
		var e [2]byte
		if _, err := io.ReadFull(c.br, e[:]); err != nil {
			return false, 0, nil, err
		}
		n = uint64(binary.BigEndian.Uint16(e[:]))
	case 127:
		var e [8]byte
		if _, err := io.ReadFull(c.br, e[:]); err != nil {
			return false, 0, nil, err
		}
		n = binary.BigEndian.Uint64(e[:])
	}
	if n > maxMessage {
		return false, 0, nil, errors.New("frame length exceeds the frame ceiling")
	}
	var key [4]byte
	if masked {
		if _, err := io.ReadFull(c.br, key[:]); err != nil {
			return false, 0, nil, err
		}
	}
	payload = make([]byte, n)
	if _, err := io.ReadFull(c.br, payload); err != nil {
		return false, 0, nil, err
	}
	if masked {
		for i := range payload {
			payload[i] ^= key[i%4]
		}
	}
	return fin, op, payload, nil
}

func (c *Conn) WriteText(b []byte) error { return c.write(OpText, b) }

func (c *Conn) WriteClose(code int, reason string) error {
	body := make([]byte, 2+len(reason))
	binary.BigEndian.PutUint16(body, uint16(code))
	copy(body[2:], reason)
	return c.write(opClose, body)
}

func (c *Conn) write(op byte, payload []byte) error {
	n := len(payload)
	head := make([]byte, 0, 14)
	head = append(head, 0x80|op)
	mb := byte(0)
	if c.mask {
		mb = 0x80
	}
	switch {
	case n < 126:
		head = append(head, mb|byte(n))
	case n <= 0xFFFF:
		head = append(head, mb|126, byte(n>>8), byte(n))
	default:
		head = append(head, mb|127)
		head = binary.BigEndian.AppendUint64(head, uint64(n))
	}
	body := payload
	if c.mask {
		var key [4]byte
		rand.Read(key[:])
		head = append(head, key[:]...)
		body = make([]byte, n)
		for i := range payload {
			body[i] = payload[i] ^ key[i%4]
		}
	}
	c.wmu.Lock()
	defer c.wmu.Unlock()
	c.c.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if _, err := c.c.Write(append(head, body...)); err != nil {
		return err
	}
	return nil
}
