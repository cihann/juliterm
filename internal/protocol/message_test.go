package protocol

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestNewMessageParseMessageRoundTrip(t *testing.T) {
	tests := []struct {
		name    string
		msgType byte
		payload []byte
	}{
		{"empty payload", MsgOutput, nil},
		{"single byte", MsgResize, []byte{0x42}},
		{"text payload", MsgError, []byte(`{"code":"E01","message":"oops"}`)},
		{"binary payload", MsgInput, []byte{0x00, 0xFF, 0x80, 0x01}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frame := NewMessage(tt.msgType, tt.payload)

			gotType, gotPayload, err := ParseMessage(frame)
			if err != nil {
				t.Fatalf("ParseMessage: %v", err)
			}
			if gotType != tt.msgType {
				t.Errorf("type = 0x%02X, want 0x%02X", gotType, tt.msgType)
			}
			if !bytes.Equal(gotPayload, tt.payload) {
				t.Errorf("payload = %v, want %v", gotPayload, tt.payload)
			}
		})
	}
}

func TestParseMessageEmpty(t *testing.T) {
	_, _, err := ParseMessage(nil)
	if err != ErrEmptyMessage {
		t.Fatalf("expected ErrEmptyMessage, got %v", err)
	}
	_, _, err = ParseMessage([]byte{})
	if err != ErrEmptyMessage {
		t.Fatalf("expected ErrEmptyMessage, got %v", err)
	}
}

func TestErrorMessageJSON(t *testing.T) {
	frame := NewErrorMessage("NOT_FOUND", "pane does not exist")

	msgType, payload, err := ParseMessage(frame)
	if err != nil {
		t.Fatalf("ParseMessage: %v", err)
	}
	if msgType != MsgError {
		t.Fatalf("type = 0x%02X, want 0x%02X", msgType, MsgError)
	}

	var ep ErrorPayload
	if err := json.Unmarshal(payload, &ep); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if ep.Code != "NOT_FOUND" {
		t.Errorf("Code = %q, want %q", ep.Code, "NOT_FOUND")
	}
	if ep.Message != "pane does not exist" {
		t.Errorf("Message = %q, want %q", ep.Message, "pane does not exist")
	}
}

func TestSessionTreeMessage(t *testing.T) {
	tree := []byte(`{"sessions":[{"id":"0","windows":[]}]}`)
	frame := NewSessionTreeMessage(tree)

	msgType, payload, err := ParseMessage(frame)
	if err != nil {
		t.Fatalf("ParseMessage: %v", err)
	}
	if msgType != MsgSessionTree {
		t.Fatalf("type = 0x%02X, want 0x%02X", msgType, MsgSessionTree)
	}
	if !bytes.Equal(payload, tree) {
		t.Errorf("payload mismatch")
	}
}

func TestResizePayloadMarshal(t *testing.T) {
	rp := ResizePayload{Cols: 120, Rows: 40}
	data, err := json.Marshal(rp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got ResizePayload
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Cols != 120 || got.Rows != 40 {
		t.Errorf("got %+v, want {Cols:120 Rows:40}", got)
	}
}

func TestHeartbeatMessage(t *testing.T) {
	frame := NewHeartbeatMessage()

	msgType, payload, err := ParseMessage(frame)
	if err != nil {
		t.Fatalf("ParseMessage: %v", err)
	}
	if msgType != MsgHeartbeat {
		t.Fatalf("type = 0x%02X, want 0x%02X", msgType, MsgHeartbeat)
	}
	if len(payload) != 0 {
		t.Errorf("payload = %v, want empty", payload)
	}
}
