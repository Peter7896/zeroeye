package ws

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/tent-of-trials/market/orderbook"
)

var ErrInvalidDeltaMessage = errors.New("invalid order book delta message")

type deltaEnvelope struct {
	Type  string          `json:"type"`
	Delta orderbook.Delta `json:"delta"`
}

func ParseOrderBookDeltaMessage(message []byte) (orderbook.Delta, error) {
	var envelope deltaEnvelope
	if err := json.Unmarshal(message, &envelope); err != nil {
		return orderbook.Delta{}, fmt.Errorf("%w: %v", ErrInvalidDeltaMessage, err)
	}
	if envelope.Type != "orderbook_delta" {
		return orderbook.Delta{}, fmt.Errorf("%w: unsupported type %q", ErrInvalidDeltaMessage, envelope.Type)
	}
	if envelope.Delta.Symbol == "" {
		return orderbook.Delta{}, fmt.Errorf("%w: missing symbol", ErrInvalidDeltaMessage)
	}
	if envelope.Delta.Sequence == 0 {
		return orderbook.Delta{}, fmt.Errorf("%w: missing sequence", ErrInvalidDeltaMessage)
	}
	return envelope.Delta, nil
}
