package orderbook

import (
	"fmt"
	"hash/fnv"
	"sort"
	"strconv"
	"time"

	"github.com/shopspring/decimal"
	"github.com/tent-of-trials/market/types"
)

// Delta is a single order book level change delivered over the WebSocket feed.
//
// A delta is applied atomically: it is fully validated before any book state is
// touched, so a rejected delta always leaves the book exactly as it was.
type Delta struct {
	Symbol   types.Symbol    `json:"symbol"`
	Side     types.OrderSide `json:"side"`
	Price    decimal.Decimal `json:"price"`
	Quantity decimal.Decimal `json:"quantity"`
	Sequence uint64          `json:"sequence"`
	Checksum string          `json:"checksum,omitempty"`
}

// Snapshot is a full book replacement used to (re)synchronise a feed.
type Snapshot struct {
	Symbol   types.Symbol  `json:"symbol"`
	Bids     []types.Level `json:"bids"`
	Asks     []types.Level `json:"asks"`
	Sequence uint64        `json:"sequence"`
}

var (
	ErrDeltaSymbol      = &BookError{"delta: missing or invalid symbol"}
	ErrDeltaSide        = &BookError{"delta: invalid side"}
	ErrDeltaPrice       = &BookError{"delta: price must be greater than zero"}
	ErrDeltaQuantity    = &BookError{"delta: quantity must not be negative"}
	ErrDeltaSequence    = &BookError{"delta: sequence must be greater than zero"}
	ErrStaleSequence    = &BookError{"delta: stale or out-of-order sequence"}
	ErrSequenceGap      = &BookError{"delta: sequence gap, snapshot required"}
	ErrChecksumMismatch = &BookError{"delta: checksum mismatch"}
)

// ValidateDelta checks a delta in isolation, without consulting or mutating book
// state. Sequencing and checksum validation happen in ApplyDelta because they
// depend on the current book.
func ValidateDelta(d Delta) error {
	if d.Symbol == "" {
		return fmt.Errorf("%w: symbol is empty", ErrDeltaSymbol)
	}
	if d.Side != types.Buy && d.Side != types.Sell {
		return fmt.Errorf("%w: got %d", ErrDeltaSide, int(d.Side))
	}
	if !d.Price.IsPositive() {
		return fmt.Errorf("%w: got %s", ErrDeltaPrice, d.Price.String())
	}
	if d.Quantity.IsNegative() {
		return fmt.Errorf("%w: got %s", ErrDeltaQuantity, d.Quantity.String())
	}
	if d.Sequence == 0 {
		return fmt.Errorf("%w: got %d", ErrDeltaSequence, d.Sequence)
	}
	return nil
}

// Sequence returns the book's current sequence number.
func (ob *OrderBook) Sequence() uint64 {
	ob.mu.RLock()
	defer ob.mu.RUnlock()
	return ob.sequence
}

// ApplySnapshot validates and atomically replaces the whole book.
//
// The book symbol is authoritative: a snapshot for another symbol is rejected
// rather than silently rebinding the book.
func (ob *OrderBook) ApplySnapshot(s Snapshot) error {
	if s.Symbol == "" {
		return fmt.Errorf("%w: symbol is empty", ErrDeltaSymbol)
	}
	if s.Symbol != ob.symbol {
		return fmt.Errorf("%w: snapshot for %s cannot be applied to book %s", ErrDeltaSymbol, s.Symbol, ob.symbol)
	}
	for _, side := range []struct {
		name   string
		levels []types.Level
	}{{"bid", s.Bids}, {"ask", s.Asks}} {
		for i, l := range side.levels {
			if !l.Price.IsPositive() {
				return fmt.Errorf("%w: %s[%d] price must be greater than zero, got %s", ErrDeltaPrice, side.name, i, l.Price.String())
			}
			if l.Quantity.IsNegative() {
				return fmt.Errorf("%w: %s[%d] quantity must not be negative, got %s", ErrDeltaQuantity, side.name, i, l.Quantity.String())
			}
		}
	}

	ob.mu.Lock()
	defer ob.mu.Unlock()

	if ob.closed {
		return ErrBookClosed
	}

	bids, err := normaliseLevels(s.Bids, true, ob.config.MaxDepth)
	if err != nil {
		return err
	}
	asks, err := normaliseLevels(s.Asks, false, ob.config.MaxDepth)
	if err != nil {
		return err
	}

	// Validation complete; commit.
	ob.bids = bids
	ob.asks = asks
	ob.orders = make(map[string]*types.Order)
	ob.sequence = s.Sequence
	ob.updatedAt = time.Now()
	return nil
}

// ApplyDelta validates a delta and, only if every check passes, mutates the
// book. Any error leaves the book untouched.
//
// Sequencing rules:
//   - sequence <= current  -> ErrStaleSequence (out-of-order / replayed)
//   - sequence > current+1 -> ErrSequenceGap (dropped update, snapshot required)
//   - sequence == current+1 -> applied
func (ob *OrderBook) ApplyDelta(d Delta) error {
	if err := ValidateDelta(d); err != nil {
		return err
	}
	if d.Symbol != ob.symbol {
		return fmt.Errorf("%w: delta for %s cannot be applied to book %s", ErrDeltaSymbol, d.Symbol, ob.symbol)
	}

	ob.mu.Lock()
	defer ob.mu.Unlock()

	if ob.closed {
		return ErrBookClosed
	}

	switch {
	case d.Sequence <= ob.sequence:
		return fmt.Errorf("%w: book at %d, delta carries %d", ErrStaleSequence, ob.sequence, d.Sequence)
	case d.Sequence > ob.sequence+1:
		return fmt.Errorf("%w: book at %d, delta carries %d", ErrSequenceGap, ob.sequence, d.Sequence)
	}

	desc := d.Side == types.Buy
	var levels []*types.Level
	if desc {
		levels = append([]*types.Level(nil), ob.bids...)
	} else {
		levels = append([]*types.Level(nil), ob.asks...)
	}

	updated, err := applyLevel(levels, d, desc)
	if err != nil {
		return err
	}
	// Keep the book at MaxDepth after every apply. Trimming happens *after* the
	// upsert so a level that is dropped for depth can still be re-added later.
	if max := ob.config.MaxDepth; max > 0 && len(updated) > max {
		updated = updated[:max]
	}

	// Determine the post-apply state of both sides so the checksum is verified
	// against the state that *would* exist, before anything is committed.
	postBids, postAsks := ob.bids, ob.asks
	if desc {
		postBids = updated
	} else {
		postAsks = updated
	}
	if d.Checksum != "" {
		if got := stateChecksum(postBids, postAsks); got != d.Checksum {
			return fmt.Errorf("%w: expected %s, computed %s", ErrChecksumMismatch, d.Checksum, got)
		}
	}

	// Every check passed; commit.
	if desc {
		ob.bids = postBids
	} else {
		ob.asks = postAsks
	}
	ob.sequence = d.Sequence
	ob.updatedAt = time.Now()
	return nil
}

// applyLevel returns a new level slice with the delta applied. A zero quantity
// removes the level (tombstone); anything else upserts it.
func applyLevel(levels []*types.Level, d Delta, desc bool) ([]*types.Level, error) {
	idx := -1
	for i, l := range levels {
		if l.Price.Equal(d.Price) {
			idx = i
			break
		}
	}
	if d.Quantity.IsZero() {
		if idx < 0 {
			return levels, nil
		}
		return append(levels[:idx], levels[idx+1:]...), nil
	}
	if idx >= 0 {
		next := make([]*types.Level, len(levels))
		copy(next, levels)
		next[idx] = &types.Level{Price: d.Price, Quantity: d.Quantity, Count: levels[idx].Count}
		return next, nil
	}
	next := append(levels, &types.Level{Price: d.Price, Quantity: d.Quantity, Count: 1})
	sort.Slice(next, func(i, j int) bool {
		if desc {
			return next[i].Price.GreaterThan(next[j].Price)
		}
		return next[i].Price.LessThan(next[j].Price)
	})
	return next, nil
}

// stateChecksum is a deterministic FNV-1a over both level sets. It is a
// checksum-style integrity check for feed divergence, not a cryptographic hash.
func stateChecksum(bids, asks []*types.Level) string {
	h := fnv.New64a()
	for _, group := range [][]*types.Level{bids, asks} {
		rows := make([]string, 0, len(group))
		for _, l := range group {
			if l == nil {
				continue
			}
			rows = append(rows, l.Price.String()+":"+l.Quantity.String())
		}
		sort.Strings(rows)
		for _, r := range rows {
			_, _ = h.Write([]byte(r))
			_, _ = h.Write([]byte(";"))
		}
		_, _ = h.Write([]byte("|"))
	}
	return strconv.FormatUint(h.Sum64(), 16)
}

// normaliseLevels converts public []types.Level into the book's internal
// pointer slice, sorted by price and trimmed to MaxDepth.
func normaliseLevels(in []types.Level, desc bool, maxDepth int) ([]*types.Level, error) {
	out := make([]*types.Level, 0, len(in))
	for _, l := range in {
		if !l.Price.IsPositive() {
			return nil, fmt.Errorf("%w: got %s", ErrDeltaPrice, l.Price.String())
		}
		if l.Quantity.IsNegative() {
			return nil, fmt.Errorf("%w: got %s", ErrDeltaQuantity, l.Quantity.String())
		}
		count := l.Count
		if count <= 0 {
			count = 1
		}
		out = append(out, &types.Level{Price: l.Price, Quantity: l.Quantity, Count: count})
	}
	sort.Slice(out, func(i, j int) bool {
		if desc {
			return out[i].Price.GreaterThan(out[j].Price)
		}
		return out[i].Price.LessThan(out[j].Price)
	})
	if maxDepth > 0 && len(out) > maxDepth {
		out = out[:maxDepth]
	}
	return out, nil
}
