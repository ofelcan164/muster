package herdr

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"strings"
	"time"
)

// Verified against a live 0.8.2 server on 2026-09-06, with (3) re-verified
// against a live 0.9.0 server on 2026-09-10. The mechanics below are not in the
// docs and three of them shape the daemon's design:
//
//  1. events.subscribe holds the connection open and replies
//     {"result":{"type":"subscription_started"}}. Live events then arrive on the
//     same connection in ~25ms.
//  2. Subscription type names are dotted ("pane.updated"); delivered event names
//     are underscored ("pane_updated"). The one exception is
//     pane.agent_status_changed, which is delivered dotted. Normalise both.
//  3. On 0.8.2, every subscribe replays the session's whole retained event
//     history before live events, paced at exactly one event per 100ms. The
//     replay includes events for panes and workspaces that have since been
//     closed, and is not causally ordered. Two subscribes a second apart
//     produced byte-identical replays. 0.9.0 dropped the replay: subscribing
//     right after a workspace was created and closed returned
//     subscription_started and nothing else, while live events kept arriving on
//     that same connection. 0.9.0's docs prescribe subscribing first and
//     buffering the stream while session.snapshot is in flight, which is the
//     order Daemon.Run already subscribes and reconciles in. Under both
//     versions events are a "something changed" hint, never a state log.
//  4. A second events.subscribe on an already-subscribed connection makes the
//     server reset it. Subscriptions are single-shot and read-only, so the set
//     of subscriptions is fixed for the life of a connection and RPCs must use
//     their own connection.
//  5. One invalid entry rejects the whole batch with invalid_request and an
//     empty id. pane.agent_status_changed, pane.output_matched and
//     pane.scroll_changed all require a pane_id and so cannot be subscribed
//     globally.
//
// Because of (4) and (5), Muster subscribes only to globally-scoped types.
// pane.updated carries the full pane record including agent, agent_status, cwd
// and terminal_title_stripped, which covers every per-pane signal Muster would
// otherwise need a per-pane subscription for.
var GlobalSubscriptions = []string{
	"pane.created",
	"pane.closed",
	"pane.updated",
	"pane.exited",
	"pane.agent_detected",
	"workspace.created",
	"workspace.closed",
	"workspace.renamed",
	"workspace.updated",
	"workspace.metadata_updated",
	"tab.created",
	"tab.closed",
	"tab.renamed",
	"worktree.created",
	"worktree.opened",
	"worktree.removed",
}

type Event struct {
	Name string
	Data json.RawMessage
}

type wireEvent struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"`
}

// normaliseEvent folds herdr's inconsistent dotted and underscored event names
// into the underscored form used everywhere else in the API.
func normaliseEvent(name string) string { return strings.ReplaceAll(name, ".", "_") }

// Subscribe holds a long-lived subscription and delivers events on the returned
// channel, reconnecting with backoff until ctx is done. The channel closes when
// the subscription stops for good.
//
// Callers must treat every event as a hint to reconcile from SessionSnapshot,
// never as a state log. 0.8.2 replayed the session's whole retained history on
// every subscribe, out of causal order and including panes that had since
// closed; 0.9.0 dropped the replay but says nothing about ordering either.
// logf may be nil. It reports why a stream ended, which is the only way a
// rejected subscription is visible: herdr refuses a whole batch when one type
// is invalid, so a herdr that drops a type Muster asks for would otherwise
// leave the daemon silently running on its five second tick alone.
func (c *Client) Subscribe(ctx context.Context, types []string, logf func(string, ...any)) <-chan Event {
	out := make(chan Event, 256)
	if logf == nil {
		logf = func(string, ...any) {}
	}
	go func() {
		defer close(out)
		backoff := 250 * time.Millisecond
		// The acknowledgement, not a returning stream, is what clears the
		// backoff. streamOnce only ever returns an error, so a reset after it
		// returned never ran at all and every dropped subscription doubled the
		// delay until reconnects stuck at the 15s ceiling.
		connected := func() { backoff = 250 * time.Millisecond }
		for ctx.Err() == nil {
			err := c.streamOnce(ctx, types, out, connected)
			if ctx.Err() != nil {
				return
			}
			logf("subscription ended: %v, retrying in %s", err, backoff)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			if backoff < 15*time.Second {
				backoff *= 2
			}
		}
	}()
	return out
}

// maxEventLine caps one subscription line. pane.updated carries a whole pane
// record and is the largest event; the biggest on a live session measured under
// 600 bytes, so 1MiB leaves room for long titles and task notes while a peer
// that never sends a newline can no longer grow the daemon until it runs out of
// memory.
const maxEventLine = 1 << 20

func (c *Client) streamOnce(ctx context.Context, types []string, out chan<- Event, connected func()) error {
	conn, err := net.DialTimeout("unix", c.socket, 3*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()

	// Close the connection as soon as the context is cancelled so the blocking
	// read below unwinds instead of hanging until the next event.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()

	subs := make([]map[string]string, 0, len(types))
	for _, t := range types {
		subs = append(subs, map[string]string{"type": t})
	}
	req, err := json.Marshal(map[string]any{
		"id":     "muster-events",
		"method": "events.subscribe",
		"params": map[string]any{"subscriptions": subs},
	})
	if err != nil {
		return err
	}
	if _, err := conn.Write(append(req, '\n')); err != nil {
		return err
	}

	// No read deadline: a healthy subscription is silent whenever the session
	// is idle, so a timeout here would tear down a working connection. The line
	// cap is what bounds memory instead. Past it Scan fails, this stream ends
	// with bufio.ErrTooLong, and Subscribe reconnects with backoff.
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 0, 1<<14), maxEventLine)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev wireEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}
		if ev.Event == "" {
			// The subscription_started acknowledgement, or an error envelope.
			var env envelope
			if json.Unmarshal(line, &env) == nil && env.Error != nil {
				return env.Error
			}
			connected()
			// Deliver the acknowledgement as an event of its own. Anything that
			// changed while the subscription was down produced no event anyone
			// received, so a reconnect has to be a reason to reconcile or those
			// changes wait for the next tick.
			ev.Event = "subscription_started"
		}
		select {
		case out <- Event{Name: normaliseEvent(ev.Event), Data: ev.Data}:
		case <-ctx.Done():
			return ctx.Err()
		default:
			// Dropping is safe: events only ever schedule a reconcile, and a
			// reconcile is already pending if the buffer is full.
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	// The server closed the stream. Report it so Subscribe backs off rather
	// than redialling in a tight loop.
	return io.EOF
}
