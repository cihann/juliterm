// Package protocol defines the WebSocket binary protocol between the Go server
// and browser clients. Every frame begins with a single command byte followed
// by the payload.
package protocol

import (
	"encoding/json"
	"errors"
)

// Message type constants. The first byte of every WebSocket binary frame.
const (
	MsgOutput      byte = 0x01 // server→client: raw PTY output
	MsgInput       byte = 0x02 // client→server: raw keyboard input
	MsgResize      byte = 0x03 // client→server: JSON {cols, rows}
	MsgSessionTree   byte = 0x04 // server→client: JSON session tree
	MsgSelectWindow  byte = 0x05 // client→server: JSON {sessionName, windowIndex}
	MsgError         byte = 0x07 // server→client: JSON error
	MsgHeartbeat     byte = 0x08 // server→client: empty liveness frame
)

// ErrEmptyMessage is returned when parsing an empty frame.
var ErrEmptyMessage = errors.New("protocol: empty message")

// ResizePayload carries terminal dimensions.
type ResizePayload struct {
	Cols int `json:"cols"`
	Rows int `json:"rows"`
}

// ErrorPayload carries a machine-readable code and a human-readable message.
type ErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// NewMessage builds a wire frame: [msgType | payload...].
func NewMessage(msgType byte, payload []byte) []byte {
	buf := make([]byte, 1+len(payload))
	buf[0] = msgType
	copy(buf[1:], payload)
	return buf
}

// ParseMessage splits a wire frame into its type byte and payload.
func ParseMessage(data []byte) (msgType byte, payload []byte, err error) {
	if len(data) == 0 {
		return 0, nil, ErrEmptyMessage
	}
	return data[0], data[1:], nil
}

// NewErrorMessage builds a MsgError frame.
func NewErrorMessage(code, message string) []byte {
	p, _ := json.Marshal(ErrorPayload{Code: code, Message: message})
	return NewMessage(MsgError, p)
}

// NewHeartbeatMessage builds an empty MsgHeartbeat frame. Browsers cannot see
// WebSocket ping frames, so the server sends these periodically to let the
// client detect a dead connection.
func NewHeartbeatMessage() []byte {
	return NewMessage(MsgHeartbeat, nil)
}

// NewSessionTreeMessage wraps pre-serialized JSON in a MsgSessionTree frame.
func NewSessionTreeMessage(tree []byte) []byte {
	return NewMessage(MsgSessionTree, tree)
}
