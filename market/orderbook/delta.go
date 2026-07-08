package orderbook

import (
	"sort"
	"time"

	"github.com/shopspring/decimal"
	"github.com/tent-of-trials/market/types"
)

// nowFn is overridable in tests; defaults to time.Now.
var nowFn = time.Now

// ApplySnapshot replaces the order book state with the given snapshot.
// Sequence is reset to 0; the next expected delta sequence is 1.
func (ob *OrderBook) ApplySnapshot(snap *types.DepthUpdate) error {
	if snap == nil {
		return ErrInvalidSnapshot
	}
	if snap.Symbol != ob.symbol {
		return ErrSymbolMismatch
	}

	ob.mu.Lock()
	defer ob.mu.Unlock()

	if ob.closed {
		return ErrBookClosed
	}

	bids := make([]*types.Level, 0, len(snap.Bids))
	for i := range snap.Bids {
		l := snap.Bids[i]
		bids = append(bids, &l)
	}
	asks := make([]*types.Level, 0, len(snap.Asks))
	for i := range snap.Asks {
		l := snap.Asks[i]
		asks = append(asks, &l)
	}

	ob.bids = sortLevelsDesc(bids)
	ob.asks = sortLevelsAsc(asks)
	ob.sequence = 0
	ob.updatedAt = nowFn()

	return nil
}

// ApplyDelta validates and applies a single per-level change.
// Returns:
//   - a *types.SequenceGapError if the sequence is stale or has a gap (book state preserved).
//   - ErrBookClosed if the book was already closed.
//   - ErrSymbolMismatch if the delta targets a different symbol.
//   - the delta's own validation error for malformed payloads (book state preserved).
func (ob *OrderBook) ApplyDelta(d *types.DepthDelta) error {
	if d == nil {
		return ErrInvalidDelta
	}
	if err := d.Validate(); err != nil {
		return err
	}

	ob.mu.Lock()
	defer ob.mu.Unlock()

	if ob.closed {
		return ErrBookClosed
	}
	if d.Symbol != ob.symbol {
		return ErrSymbolMismatch
	}

	// Sequence gating:
	//   - First delta after snapshot / fresh book: accept as baseline (any sequence).
	//   - Subsequent deltas: must be exactly the next sequence (ob.sequence + 1).
	//   - Anything <= ob.sequence is stale / replay duplicate.
	if ob.sequence == 0 {
		// Accept as baseline; caller is responsible for re-snapshotting if upstream
		// restarted publishing from a different point.
	} else {
		if d.Sequence <= ob.sequence {
			return &types.SequenceGapError{Expected: ob.sequence, Got: d.Sequence}
		}
		if d.Sequence != ob.sequence+1 {
			return &types.SequenceGapError{Expected: ob.sequence + 1, Got: d.Sequence}
		}
	}

	if d.Side == types.Buy {
		ob.bids = upsertLevel(ob.bids, d.Price, d.Quantity, true)
	} else {
		ob.asks = upsertLevel(ob.asks, d.Price, d.Quantity, false)
	}

	ob.sequence = d.Sequence
	ob.updatedAt = nowFn()
	return nil
}

// Sequence returns the last applied sequence number (0 before any delta/snapshot-resync).
func (ob *OrderBook) Sequence() uint64 {
	ob.mu.RLock()
	defer ob.mu.RUnlock()
	return ob.sequence
}

// upsertLevel inserts/updates/removes a price level in-place and re-sorts.
// bids=true sorts descending (best bid first); asks sort ascending (best ask first).
func upsertLevel(levels []*types.Level, price, qty decimal.Decimal, desc bool) []*types.Level {
	// Remove any existing level at this price.
	for i, l := range levels {
		if l.Price.Equal(price) {
			levels = append(levels[:i], levels[i+1:]...)
			break
		}
	}
	if qty.IsZero() {
		// Removal only.
		return sortLevels(levels, desc)
	}
	level := &types.Level{Price: price, Quantity: qty, Count: 1}
	levels = append(levels, level)
	return sortLevels(levels, desc)
}

func sortLevels(levels []*types.Level, desc bool) []*types.Level {
	out := make([]*types.Level, len(levels))
	copy(out, levels)
	sortLevelsInPlace(out, desc)
	return out
}

func sortLevelsInPlace(levels []*types.Level, desc bool) {
	sort.Slice(levels, func(i, j int) bool {
		if desc {
			return levels[i].Price.GreaterThan(levels[j].Price)
		}
		return levels[i].Price.LessThan(levels[j].Price)
	})
}

func sortLevelsDesc(levels []*types.Level) []*types.Level { return sortLevels(levels, true) }
func sortLevelsAsc(levels []*types.Level) []*types.Level  { return sortLevels(levels, false) }
