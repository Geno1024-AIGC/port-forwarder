package tunnel

import (
	"encoding/json"
	"fmt"
)

// Frame is a single newline-delimited JSON control message.
type Frame struct {
	Type    string `json:"type"`
	ID      string `json:"id,omitempty"`
	Name    string `json:"name,omitempty"`
	Listen  string `json:"listen,omitempty"`
	Target  string `json:"target,omitempty"`
	ConnID  uint64 `json:"connID,omitempty"`
	Message string `json:"message,omitempty"`
}

const (
	FrameRegister   = "register"
	FrameRegistered = "registered"
	FrameUnregister = "unregister"
	FrameOpen       = "open"
	FramePing       = "ping"
	FramePong       = "pong"
	FrameError      = "error"
)

const dataPrefix = "DATA "

// dataHeader formats the handshake line of a data connection.
func dataHeader(connID uint64) string {
	return fmt.Sprintf("%s%d\n", dataPrefix, connID)
}

func parseDataHeader(line string) (uint64, bool) {
	if len(line) < len(dataPrefix)+1 {
		return 0, false
	}
	var id uint64
	if _, err := fmt.Sscanf(line[len(dataPrefix):], "%d", &id); err != nil {
		return 0, false
	}
	return id, true
}

func encodeFrame(f Frame) ([]byte, error) {
	b, err := json.Marshal(f)
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}
