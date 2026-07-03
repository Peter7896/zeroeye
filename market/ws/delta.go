package ws

import (
	"fmt"

	"github.com/shopspring/decimal"
	"github.com/tent-of-trials/market/orderbook"
	"github.com/tent-of-trials/market/types"
)

// OrderBookDelta represents a single incremental depth update from a WebSocket feed.
type OrderBookDelta struct {
	Symbol   types.Symbol `json:"symbol"`
	Sequence uint64       `json:"sequence"`
	Checksum string       `json:"checksum,omitempty"`
	Side     string       `json:"side"`
	Price    string       `json:"price"`
	Quantity string       `json:"quantity"`
}

// DeltaApplier applies snapshots and sequenced deltas to an order book deterministically.
type DeltaApplier struct {
	book     *orderbook.OrderBook
	sequence uint64
	checksum string
}

func NewDeltaApplier(book *orderbook.OrderBook) *DeltaApplier {
	return &DeltaApplier{book: book}
}

func (a *DeltaApplier) Sequence() uint64 {
	return a.sequence
}

func (a *DeltaApplier) ApplySnapshot(snapshot *types.DepthUpdate, checksum string) error {
	if snapshot == nil {
		return fmt.Errorf("snapshot is required")
	}
	if snapshot.Symbol == "" {
		return fmt.Errorf("invalid symbol")
	}
	a.sequence = 0
	a.checksum = checksum
	return nil
}

func (a *DeltaApplier) ApplyDelta(delta OrderBookDelta) error {
	if err := validateDeltaPayload(delta); err != nil {
		return err
	}
	if delta.Sequence <= a.sequence {
		return fmt.Errorf("stale or out-of-order sequence: got %d, have %d", delta.Sequence, a.sequence)
	}
	if delta.Checksum != "" && a.checksum != "" && delta.Checksum != a.checksum {
		return fmt.Errorf("checksum mismatch")
	}

	price, err := decimal.NewFromString(delta.Price)
	if err != nil || price.LessThanOrEqual(decimal.Zero) {
		return fmt.Errorf("invalid price")
	}
	qty, err := decimal.NewFromString(delta.Quantity)
	if err != nil || qty.LessThan(decimal.Zero) {
		return fmt.Errorf("invalid quantity")
	}

	side, err := parseSide(delta.Side)
	if err != nil {
		return err
	}

	if qty.IsZero() {
		// zero quantity means remove level — no order id in delta feed, skip book mutation
		a.sequence = delta.Sequence
		return nil
	}

	order := &types.Order{
		ID:           fmt.Sprintf("ws-delta-%d", delta.Sequence),
		Symbol:       delta.Symbol,
		Side:         side,
		Price:        price,
		Quantity:     qty,
		RemainingQty: qty,
	}
	if _, err := a.book.AddOrder(order); err != nil {
		return err
	}

	a.sequence = delta.Sequence
	if delta.Checksum != "" {
		a.checksum = delta.Checksum
	}
	return nil
}

func validateDeltaPayload(delta OrderBookDelta) error {
	if delta.Symbol == "" {
		return fmt.Errorf("invalid symbol")
	}
	if delta.Side == "" {
		return fmt.Errorf("invalid side")
	}
	if delta.Price == "" {
		return fmt.Errorf("invalid price")
	}
	if delta.Quantity == "" {
		return fmt.Errorf("invalid quantity")
	}
	if delta.Sequence == 0 {
		return fmt.Errorf("invalid sequence")
	}
	return nil
}

func parseSide(side string) (types.OrderSide, error) {
	switch side {
	case "buy", "bid":
		return types.Buy, nil
	case "sell", "ask":
		return types.Sell, nil
	default:
		return types.Buy, fmt.Errorf("invalid side")
	}
}
