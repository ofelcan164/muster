package herdr

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeServer listens on a unix socket and runs handle on each connection after
// reading its request line. The socket lives under os.MkdirTemp("", "h")
// because unix socket paths cap near 108 bytes and t.TempDir paths carry the
// test name.
func fakeServer(t *testing.T, handle func(conn net.Conn, id, method string)) *Client {
	t.Helper()
	dir, err := os.MkdirTemp("", "h")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				line, err := bufio.NewReader(conn).ReadBytes('\n')
				if err != nil {
					return
				}
				var req struct{ ID, Method string }
				if json.Unmarshal(line, &req) != nil {
					return
				}
				handle(conn, req.ID, req.Method)
			}()
		}
	}()
	return NewClient(sock)
}

func TestCallDecodesResult(t *testing.T) {
	c := fakeServer(t, func(conn net.Conn, id, method string) {
		fmt.Fprintf(conn, `{"id":%q,"result":{"method":%q,"n":3}}`+"\n", id, method)
	})
	var out struct {
		Method string `json:"method"`
		N      int    `json:"n"`
	}
	if err := c.Call("pane.list", nil, &out); err != nil {
		t.Fatal(err)
	}
	if out.Method != "pane.list" || out.N != 3 {
		t.Fatalf("got %+v, want method pane.list and n 3", out)
	}
}

// The overlay picks toggle over open on this code, so it has to survive a
// caller wrapping the error with %w.
func TestCallErrorCarriesCode(t *testing.T) {
	c := fakeServer(t, func(conn net.Conn, id, _ string) {
		fmt.Fprintf(conn, `{"id":%q,"error":{"code":"plugin_pane_not_found","message":"gone"}}`+"\n", id)
	})
	err := c.Call("plugin.pane.focus", nil, nil)
	if got := Code(err); got != "plugin_pane_not_found" {
		t.Fatalf("Code(%v) = %q", err, got)
	}
	if got := Code(fmt.Errorf("toggle: %w", err)); got != "plugin_pane_not_found" {
		t.Fatalf("Code of wrapped error = %q", got)
	}
	if got := Code(errors.New("other")); got != "" {
		t.Fatalf("Code of a non-API error = %q, want empty", got)
	}
}

func TestCallRejectsOversizedReply(t *testing.T) {
	c := fakeServer(t, func(conn net.Conn, id, _ string) {
		fmt.Fprintf(conn, `{"id":%q,"result":"%s"}`+"\n", id, strings.Repeat("a", maxReply))
	})
	err := c.Call("session.snapshot", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "reply over") {
		t.Fatalf("err = %v, want the reply-over-cap error", err)
	}
}
