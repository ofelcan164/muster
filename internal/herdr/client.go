// Package herdr is a thin client for the herdr 0.9.0 unix socket API.
//
// Everything the daemon needs is on the socket, so nothing here shells out to
// the herdr binary. Requests are newline-delimited JSON objects; every reply is
// either {"id":..,"result":{..}} or {"id":..,"error":{..}}.
package herdr

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"
)

// SocketPath resolves the server socket, preferring the value herdr injects
// into plugin commands so a daemon started by a hook always talks to the server
// that started it.
func SocketPath() string {
	if p := os.Getenv("HERDR_SOCKET_PATH"); p != "" {
		return p
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".config", "herdr", "herdr.sock")
	}
	return ""
}

type Client struct {
	socket string
	seq    atomic.Uint64
}

func NewClient(socket string) *Client {
	if socket == "" {
		socket = SocketPath()
	}
	return &Client{socket: socket}
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *apiError) Error() string { return fmt.Sprintf("herdr %s: %s", e.Code, e.Message) }

// Code returns the herdr error code for err, or "" if err is not an API error.
// It looks through %w wraps, so a caller adding context keeps the code visible.
func Code(err error) string {
	var e *apiError
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

// maxReply caps one Call reply. session.snapshot is the largest, and a live
// 7-pane session measured about 10KB, so 16MiB covers sessions of thousands of
// panes while still stopping a peer that never sends a newline.
const maxReply = 16 << 20

type envelope struct {
	ID     string          `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *apiError       `json:"error"`
}

// Call performs one request/response round trip on its own connection.
//
// A connection is never reused: herdr resets a connection that receives a
// second events.subscribe, and keeping RPC traffic off the subscription
// connection removes any chance of tripping that. Unix socket connects cost
// microseconds, and the daemon's call rate is low.
func (c *Client) Call(method string, params any, out any) error {
	if params == nil {
		params = struct{}{}
	}
	conn, err := net.DialTimeout("unix", c.socket, 3*time.Second)
	if err != nil {
		return fmt.Errorf("dial %s: %w", c.socket, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

	id := fmt.Sprintf("muster-%d", c.seq.Add(1))
	req := map[string]any{"id": id, "method": method, "params": params}
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	if _, err := conn.Write(append(body, '\n')); err != nil {
		return fmt.Errorf("%s: write: %w", method, err)
	}

	// ReadBytes accumulates across fills, so the buffer size bounds nothing but
	// the syscall count. A megabyte of it per call was 150MiB/min of garbage on
	// a busy session; 16KiB reads a real snapshot in a fill or two and is faster.
	// The LimitReader is what bounds memory.
	r := bufio.NewReaderSize(io.LimitReader(conn, maxReply), 1<<14)
	line, err := r.ReadBytes('\n')
	if err != nil && len(line) == maxReply {
		// The cap cut the reply short. Decoding the prefix would only fail with
		// a parse error that hides the real cause.
		return fmt.Errorf("%s: reply over %d bytes", method, maxReply)
	}
	if err != nil && len(line) == 0 {
		return fmt.Errorf("%s: read: %w", method, err)
	}
	var env envelope
	if err := json.Unmarshal(line, &env); err != nil {
		return fmt.Errorf("%s: decode: %w", method, err)
	}
	if env.Error != nil {
		return env.Error
	}
	if out != nil {
		return json.Unmarshal(env.Result, out)
	}
	return nil
}
