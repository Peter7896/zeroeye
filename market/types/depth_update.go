package types

import (
	"fmt"
	"strings"

	"github.com/shopspring/decimal"
)

// DepthDelta represents a single per-level change to the order book.
//
// Quantity == 0 signals level removal.
// Sequence numbers must be strictly increasing; gaps are errors (caller must
// re-snapshot). Sequence <= lastApplied is a stale/replay duplicate and is dropped.
type DepthDelta struct {
	Symbol    Symbol          `json:"symbol"`
	Side      OrderSide       `json:"side"`
	Price     decimal.Decimal `json:"price"`
	Quantity  decimal.Decimal `json:"quantity"`
	Sequence  uint64          `json:"sequence"`
	Timestamp int64           `json:"timestamp"`
}

// Validate enforces the wire-format schema before the order book touches it.
// Errors are descriptive enough for upstream logging.
func (d *DepthDelta) Validate() error {
	if strings.TrimSpace(string(d.Symbol)) == "" {
		return fmt.Errorf("invalid depth delta: empty symbol")
	}
	if d.Side != Buy && d.Side != Sell {
		return fmt.Errorf("invalid depth delta: unknown side %d", d.Side)
	}
	if d.Price.LessThanOrEqual(decimal.Zero) {
		return fmt.Errorf("invalid depth delta: price must be > 0, got %s", d.Price)
	}
	if d.Quantity.LessThan(decimal.Zero) {
		return fmt.Errorf("invalid depth delta: quantity must be >= 0, got %s", d.Quantity)
	}
	if d.Sequence == 0 {
		return fmt.Errorf("invalid depth delta: sequence must be > 0")
	}
	if d.Timestamp <= 0 {
		return fmt.Errorf("invalid depth delta: timestamp must be > 0")
	}
	return nil
}

// SequenceGapError is returned when a delta arrives out of order or with a gap.
//
// When Got <= Expected the error represents a stale/replay duplicate.
// When Got > Expected+1 the error represents a true gap (caller should re-snapshot).
type SequenceGapError struct {
	Expected uint64
	Got      uint64
}

// Gap is Got - Expected (always positive).
func (e *SequenceGapError) Gap() uint64 {
	if e.Got > e.Expected {
		return e.Got - e.Expected
	}
	return 0
}

// IsStale reports whether the delta is older-than-or-equal to the last applied sequence.
func (e *SequenceGapError) IsStale() bool {
	return e.Got <= e.Expected
}

func (e *SequenceGapError) Error() string {
	if e.IsStale() {
		return fmt.Sprintf("stale depth delta: got sequence %d, expected > %d", e.Got, e.Expected)
	}
	return fmt.Sprintf("sequence gap: got %d, expected %d (gap=%d)", e.Got, e.Expected, e.Gap())
}
