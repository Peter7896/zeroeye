# Fix for Issue #4: [$45 BOUNTY] [Go] Add WebSocket order book delta validation tests

// market/ws/test_helpers.go
package ws

import (
	"encoding/json"
	"errors"
	"sync"
)

// MockWebSocketConn provides a deterministic WebSocket connection for testing
type MockWebSocketConn struct {
	mu           sync.Mutex
	messages     [][]byte
	messageIndex int
	closed       bool
	writeBuffer  [][]byte
}

// NewMockWebSocketConn creates a new mock connection with predefined messages
func NewMockWebSocketConn(messages [][]byte) *MockWebSocketConn {
	return &MockWebSocketConn{
		messages:     messages,
		messageIndex: 0,
		writeBuffer:  make([][]byte, 0),
	}
}

// ReadMessage returns the next predefined message
func (m *MockWebSocketConn) ReadMessage() (messageType int, p []byte, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return 0, nil, errors.New("connection closed")
	}

	if m.messageIndex >= len(m.messages) {
		return 0, nil, errors.New("no more messages")
	}

	msg := m.messages[m.messageIndex]
	m.messageIndex++
	return 1, msg, nil // 1 = TextMessage
}

// WriteMessage stores the message in the write buffer
func (m *MockWebSocketConn) WriteMessage(messageType int, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return errors.New("connection closed")
	}

	m.writeBuffer = append(m.writeBuffer, data)
	return nil
}

// Close marks the connection as closed
func (m *MockWebSocketConn) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

// GetWrittenMessages returns all messages written to the connection
func (m *MockWebSocketConn) GetWrittenMessages() [][]byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.writeBuffer
}

// OrderBookDelta represents a delta update to the order book
type OrderBookDelta struct {
	Type      string          `json:"type"`
	Symbol    string          `json:"symbol"`
	Sequence  int64           `json:"sequence"`
	Bids      [][]interface{} `json:"bids,omitempty"`
	Asks      [][]interface{} `json:"asks,omitempty"`
	Timestamp int64           `json:"timestamp"`
	Checksum  string          `json:"checksum,omitempty"`
}

// OrderBookSnapshot represents a full order book snapshot
type OrderBookSnapshot struct {
	Type      string          `json:"type"`
	Symbol    string          `json:"symbol"`
	Sequence  int64           `json:"sequence"`
	Bids      [][]interface{} `json:"bids"`
	Asks      [][]interface{} `json:"asks"`
	Timestamp int64           `json:"timestamp"`
	Checksum  string          `json:"checksum,omitempty"`
}

// CreateDeltaMessage creates a JSON-encoded delta message
func CreateDeltaMessage(delta OrderBookDelta) []byte {
	data, _ := json.Marshal(delta)
	return data
}

// CreateSnapshotMessage creates a JSON-encoded snapshot message
func CreateSnapshotMessage(snapshot OrderBookSnapshot) []byte {
	data, _ := json.Marshal(snapshot)
	return data
}

// DeltaValidationError represents a validation error for delta processing
type DeltaValidationError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
}

func (e *DeltaValidationError) Error() string {
	return e.Message
}

// ValidationErrorCodes for delta validation
const (
	ErrCodeMalformedPrice    = "MALFORMED_PRICE"
	ErrCodeMalformedQuantity = "MALFORMED_QUANTITY"
	ErrCodeMalformedSide     = "MALFORMED_SIDE"
	ErrCodeMalformedSymbol   = "MALFORMED_SYMBOL"
	ErrCodeStaleSequence     = "STALE_SEQUENCE"
	ErrCodeOutOfOrder        = "OUT_OF_ORDER_SEQUENCE"
	ErrCodeChecksumMismatch  = "CHECKSUM_MISMATCH"
	ErrCodeInvalidPayload    = "INVALID_PAYLOAD"
)