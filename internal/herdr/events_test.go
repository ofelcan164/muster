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
// names normalised and without the subscription_started ack among them.
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
	ch := c.Subscribe(ctx, GlobalSubscriptions)
	defer func() {
		cancel()
		for range ch {
		}
	}()

	for _, want := range []string{"pane_agent_status_changed", "pane_updated"} {
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
