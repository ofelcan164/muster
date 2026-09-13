package herdr

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The first connection streams a line with no end. The client has to drop it
// and reconnect, and the second connection's events have to arrive with their
// names normalised. Each connection announces itself with a subscription_started
// event first, which is what makes the daemon reconcile after a reconnect.
func TestSubscribeNormalisesAndReconnectsPastLongLine(t *testing.T) {
	var conns atomic.Int32
	c := fakeServer(t, func(conn net.Conn, id, method string) {
		if method != "events.subscribe" {
			return
		}
		fmt.Fprintf(conn, `{"id":%q,"result":{"type":"subscription_started"}}`+"\n", id)
		if conns.Add(1) == 1 {
			conn.Write(bytes.Repeat([]byte("x"), maxEventLine+1))
		} else {
			fmt.Fprint(conn, `{"event":"pane.agent_status_changed","data":{"pane_id":"p1"}}`+"\n")
			fmt.Fprint(conn, `{"event":"pane_updated","data":{}}`+"\n")
		}
		// Hold the connection open, so only the client can end it.
		io.Copy(io.Discard, conn)
	})

	ctx, cancel := context.WithCancel(context.Background())
	ch := c.Subscribe(ctx, GlobalSubscriptions, nil)
	defer func() {
		cancel()
		for range ch {
		}
	}()

	for _, want := range []string{
		"subscription_started", // the dropped connection
		"subscription_started", // the one that replaced it
		"pane_agent_status_changed",
		"pane_updated",
	} {
		select {
		case ev := <-ch:
			if ev.Name != want {
				t.Fatalf("event %q, want %q", ev.Name, want)
			}
			if want == "pane_agent_status_changed" && !strings.Contains(string(ev.Data), "p1") {
				t.Fatalf("data %s lost the pane id", ev.Data)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("no %s event", want)
		}
	}
	if n := conns.Load(); n != 2 {
		t.Fatalf("%d connections, want 2", n)
	}
}

// A subscription that keeps being dropped has to keep reconnecting promptly.
// The backoff used to double on every drop and never reset, because the reset
// sat after a call that only ever returns an error, so a session that dropped a
// handful of subscriptions ended up waiting 15s to notice anything at all.
func TestSubscribeResetsBackoffOnEachConnection(t *testing.T) {
	var conns atomic.Int32
	c := fakeServer(t, func(conn net.Conn, id, method string) {
		if method != "events.subscribe" {
			return
		}
		fmt.Fprintf(conn, `{"id":%q,"result":{"type":"subscription_started"}}`+"\n", id)
		conns.Add(1)
		// Drop it immediately, every time.
	})

	ctx, cancel := context.WithCancel(context.Background())
	ch := c.Subscribe(ctx, GlobalSubscriptions, nil)
	defer func() {
		cancel()
		for range ch {
		}
	}()

	// Six reconnects at the 250ms floor take about 1.5s. Doubling without a
	// reset would need more than 15s to get this far.
	deadline := time.After(6 * time.Second)
	for conns.Load() < 6 {
		select {
		case <-ch:
		case <-deadline:
			t.Fatalf("only %d connections: the backoff is not resetting", conns.Load())
		}
	}
}
