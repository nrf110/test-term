// Package client connects to an engine server over WebSocket and presents it as
// a local handle: a mirror engine.Session fed by the event stream, plus
// Run/Cancel commands. The TUI consumes the mirror exactly as it would an
// in-process engine, so local and remote use one code path.
package client

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/nrf110/test-term/internal/engine"
	"github.com/nrf110/test-term/internal/protocol"
)

// Client is a connection to a remote (or loopback) engine server.
type Client struct {
	conn *websocket.Conn
	eng  *engine.Session

	ctx    context.Context
	cancel context.CancelFunc

	wmu sync.Mutex // serializes writes
}

// Dial connects to addr (host:port), performs the initial snapshot handshake,
// and starts mirroring the event stream into a local engine. token, if
// non-empty, is sent as a bearer credential. dialCtx bounds the connect; the
// connection then lives until Close.
func Dial(dialCtx context.Context, addr, token string) (*Client, error) {
	opts := &websocket.DialOptions{}
	if token != "" {
		opts.HTTPHeader = http.Header{"Authorization": []string{"Bearer " + token}}
	}
	url := "ws://" + addr + "/ws"
	conn, _, err := websocket.Dial(dialCtx, url, opts)
	if err != nil {
		return nil, fmt.Errorf("connecting to %s: %w", addr, err)
	}

	// The first frame must be the snapshot, which seeds the mirror.
	var first protocol.Message
	if err := wsjson.Read(dialCtx, conn, &first); err != nil {
		_ = conn.CloseNow()
		return nil, fmt.Errorf("reading initial snapshot: %w", err)
	}
	if first.Type != protocol.MsgSnapshot {
		_ = conn.CloseNow()
		return nil, fmt.Errorf("expected snapshot, got %q", first.Type)
	}

	ctx, cancel := context.WithCancel(context.Background())
	c := &Client{
		conn:   conn,
		eng:    engine.NewFromSnapshot(first.Snapshot),
		ctx:    ctx,
		cancel: cancel,
	}
	go c.readLoop()
	return c, nil
}

// Engine returns the mirror session the client keeps in sync with the server.
func (c *Client) Engine() *engine.Session { return c.eng }

// readLoop applies streamed events to the mirror until the connection ends.
func (c *Client) readLoop() {
	for {
		var msg protocol.Message
		if err := wsjson.Read(c.ctx, c.conn, &msg); err != nil {
			return
		}
		if msg.Type == protocol.MsgEvent && msg.Event != nil {
			c.eng.Apply(*msg.Event)
		}
	}
}

// Run requests a run for the selection (non-blocking; results stream back).
func (c *Client) Run(sel engine.Selection) {
	_ = c.write(protocol.Command{Type: protocol.CmdRun, Selection: sel})
}

// Cancel requests cancellation of the in-flight run.
func (c *Client) Cancel() {
	_ = c.write(protocol.Command{Type: protocol.CmdCancel})
}

func (c *Client) write(cmd protocol.Command) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	return wsjson.Write(c.ctx, c.conn, cmd)
}

// Close tears down the connection and stops mirroring.
func (c *Client) Close() error {
	c.cancel()
	return c.conn.Close(websocket.StatusNormalClosure, "")
}
