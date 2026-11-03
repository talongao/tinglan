// Package wire implements the Content-Length framing shared by the host and
// plugin sides of the source plugin protocol.
package wire

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// MaxFrame is the largest single message, matching the host transport limit.
const MaxFrame = 16 << 20

// Error is a JSON-RPC error object.
type Error struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("rpc %d: %s", e.Code, e.Message)
}

// Message is one JSON-RPC 2.0 request, notification/response.
type Message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

// IsResponse reports whether the message answers a pending call.
func (m Message) IsResponse() bool { return m.Method == "" && len(m.ID) > 0 }

// IDString decodes the id into the string form used by both peers.
func (m Message) IDString() string {
	var id string
	if err := json.Unmarshal(m.ID, &id); err == nil {
		return id
	}
	return strings.Trim(string(m.ID), `"`)
}

// Read decodes one framed message.
func Read(r *bufio.Reader) (Message, error) {
	var msg Message
	length := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return msg, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			return msg, errors.New("wire: malformed header")
		}
		if strings.EqualFold(strings.TrimSpace(name), "content-length") {
			n, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil || n < 0 || n > MaxFrame {
				return msg, errors.New("wire: invalid content length")
			}
			length = n
		}
	}
	if length < 0 {
		return msg, errors.New("wire: missing content length")
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return msg, err
	}
	if err := json.Unmarshal(body, &msg); err != nil {
		return msg, fmt.Errorf("wire: invalid json: %w", err)
	}
	if msg.JSONRPC != "2.0" {
		return msg, errors.New("wire: unsupported jsonrpc version")
	}
	return msg, nil
}

// Write encodes one framed message.
func Write(w io.Writer, msg Message) error {
	if msg.JSONRPC == "" {
		msg.JSONRPC = "2.0"
	}
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	if len(body) > MaxFrame {
		return errors.New("wire: message too large")
	}
	if _, err := fmt.Fprintf(w, "Content-Length: %d\r\n\r\n", len(body)); err != nil {
		return err
	}
	_, err = w.Write(body)
	return err
}

// Request builds a request message with a string id.
func Request(id, method string, params any) (Message, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return Message{}, err
	}
	idRaw, _ := json.Marshal(id)
	return Message{JSONRPC: "2.0", ID: idRaw, Method: method, Params: raw}, nil
}

// Result builds a success response.
func Result(id string, result any) (Message, error) {
	raw, err := json.Marshal(result)
	if err != nil {
		return Message{}, err
	}
	idRaw, _ := json.Marshal(id)
	return Message{JSONRPC: "2.0", ID: idRaw, Result: raw}, nil
}

// Failure builds an error response.
func Failure(id string, e *Error) Message {
	idRaw, _ := json.Marshal(id)
	return Message{JSONRPC: "2.0", ID: idRaw, Error: e}
}

// Notification builds a notification (no id).
func Notification(method string, params any) (Message, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return Message{}, err
	}
	return Message{JSONRPC: "2.0", Method: method, Params: raw}, nil
}
