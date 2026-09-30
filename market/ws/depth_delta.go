package ws

import (
	"encoding/json"
	"fmt"

	"github.com/shopspring/decimal"
	"github.com/tent-of-trials/market/orderbook"
	"github.com/tent-of-trials/market/types"
)

// ApplyDepthDelta validates a decoded feed message before changing the book.
// It is a pure feed helper: callers choose the book and transport; this does not
// enable inbound trading or change the server's subscription protocol.
func ApplyDepthDelta(book *orderbook.OrderBook, payload []byte) error {
	if book == nil {
		return fmt.Errorf("depth delta: missing order book")
	}
	var wire struct {
		Symbol           *types.Symbol `json:"symbol"`
		PreviousSequence *uint64       `json:"previous_sequence"`
		Sequence         *uint64       `json:"sequence"`
		Updates          *[]struct {
			Side     *string          `json:"side"`
			Price    *decimal.Decimal `json:"price"`
			Quantity *decimal.Decimal `json:"quantity"`
			Count    int64            `json:"order_count,omitempty"`
		} `json:"updates"`
		Checksum string `json:"checksum,omitempty"`
	}
	if err := json.Unmarshal(payload, &wire); err != nil {
		return fmt.Errorf("depth delta: invalid JSON payload: %w", err)
	}
	if wire.Symbol == nil || *wire.Symbol == "" {
		return fmt.Errorf("depth delta: symbol is required")
	}
	if wire.PreviousSequence == nil || wire.Sequence == nil {
		return fmt.Errorf("depth delta: previous_sequence and sequence are required")
	}
	if wire.Updates == nil {
		return fmt.Errorf("depth delta: updates array is required")
	}
	delta := orderbook.DepthDelta{Symbol: *wire.Symbol, PreviousSequence: *wire.PreviousSequence, Sequence: *wire.Sequence, Checksum: wire.Checksum}
	for i, update := range *wire.Updates {
		// Missing/null quantity must not become the zero-quantity deletion command.
		if update.Side == nil || update.Price == nil || update.Quantity == nil {
			return fmt.Errorf("depth delta: update %d requires side, price and quantity", i)
		}
		if update.Count < 0 {
			return fmt.Errorf("depth delta: update %d has negative order_count", i)
		}
		delta.Updates = append(delta.Updates, orderbook.DepthDeltaLevel{Side: *update.Side, Price: *update.Price, Quantity: *update.Quantity, Count: update.Count})
	}
	return book.ApplyDelta(delta)
}
