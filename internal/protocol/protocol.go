// Package protocol defines the JSON wire format spoken over the WebSocket
// between the engine server and its clients (TUI, and later alternate clients).
// It reuses the engine and event types directly so the wire stays in lockstep
// with the in-process model.
package protocol

import (
	"github.com/nrf110/test-term/internal/engine"
	"github.com/nrf110/test-term/internal/event"
)

// Command types (client -> server).
const (
	CmdRun    = "run"
	CmdCancel = "cancel"
)

// Message types (server -> client).
const (
	MsgSnapshot = "snapshot"
	MsgEvent    = "event"
)

// Command is a request from a client to the engine server.
type Command struct {
	Type string `json:"type"`
	// Selection is meaningful for CmdRun.
	Selection engine.Selection `json:"selection,omitempty"`
}

// Message is a frame from the server to a client. On connect the server sends
// one MsgSnapshot, then a stream of MsgEvent frames.
type Message struct {
	Type     string         `json:"type"`
	Snapshot []*engine.Node `json:"snapshot,omitempty"`
	Event    *event.Event   `json:"event,omitempty"`
}
