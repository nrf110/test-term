// Package server exposes an engine over HTTP: a WebSocket endpoint that streams
// the session snapshot plus live events to each client and accepts run/cancel
// commands. The same server hosts the MCP endpoint in a later phase.
//
// Security: the server executes arbitrary test code on behalf of callers, so
// callers are responsible for binding it to a loopback address by default. When
// bound to a non-loopback address a token must be required (enforced by the
// command layer); this package checks the token when one is configured.
package server

import (
	"context"
	"crypto/subtle"
	"net/http"
	"sync"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/nrf110/test-term/internal/engine"
	"github.com/nrf110/test-term/internal/protocol"
)

// Commander executes run/cancel requests. *runner.Runner satisfies it.
type Commander interface {
	Run(sel engine.Selection)
	Cancel()
}

// Server serves the engine's WebSocket protocol.
type Server struct {
	eng   *engine.Session
	ctrl  Commander
	token string
}

// New returns a Server. If token is non-empty, WebSocket connections must
// present it as "Authorization: Bearer <token>".
func New(eng *engine.Session, ctrl Commander, token string) *Server {
	return &Server{eng: eng, ctrl: ctrl, token: token}
}

// Handler returns the HTTP handler (currently just /ws). Later phases mount the
// MCP endpoint on the same mux.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleWS)
	return mux
}

func (s *Server) authorized(r *http.Request) bool {
	if s.token == "" {
		return true
	}
	// Constant-time comparison avoids leaking the token via response timing.
	expected := "Bearer " + s.token
	got := r.Header.Get("Authorization")
	return subtle.ConstantTimeCompare([]byte(got), []byte(expected)) == 1
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Default Accept options reject cross-origin browser requests (Origin != Host)
	// while permitting non-browser clients, which send no Origin. This is the
	// WebSocket CSRF guard — important because this endpoint runs arbitrary test
	// code, so a malicious web page must never be able to drive it.
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer func() { _ = c.CloseNow() }()

	ctx := r.Context()
	sub := s.eng.Subscribe(1024)
	defer sub.Close()

	var wmu sync.Mutex
	write := func(m protocol.Message) error {
		wmu.Lock()
		defer wmu.Unlock()
		return wsjson.Write(ctx, c, m)
	}

	// First frame: the current tree.
	if err := write(protocol.Message{Type: protocol.MsgSnapshot, Snapshot: sub.Snapshot}); err != nil {
		return
	}

	// Read commands concurrently; the reader exits when the connection closes.
	go s.readCommands(ctx, c)

	// Stream events until the client disconnects or the subscription ends.
	for {
		select {
		case e := <-sub.C:
			ev := e
			if err := write(protocol.Message{Type: protocol.MsgEvent, Event: &ev}); err != nil {
				return
			}
		case <-sub.Done():
			return
		case <-ctx.Done():
			return
		}
	}
}

func (s *Server) readCommands(ctx context.Context, c *websocket.Conn) {
	for {
		var cmd protocol.Command
		if err := wsjson.Read(ctx, c, &cmd); err != nil {
			return
		}
		switch cmd.Type {
		case protocol.CmdRun:
			s.ctrl.Run(cmd.Selection)
		case protocol.CmdCancel:
			s.ctrl.Cancel()
		}
	}
}
