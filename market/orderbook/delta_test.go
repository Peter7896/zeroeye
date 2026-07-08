package orderbook

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/tent-of-trials/market/types"
)

// helper: build an order book against BTCUSDT
func newTestBook() *OrderBook {
	return NewOrderBook(types.Symbol("BTCUSDT"), Config{
		MaxDepth:       10,
		PriceDecimals:  2,
		VolumeDecimals: 8,
	})
}

// TestApplyDelta_HappyPath_InsertBid verifies a fresh delta inserts a bid level.
func TestApplyDelta_HappyPath_InsertBid(t *testing.T) {
	ob := newTestBook()
	d := &types.DepthDelta{
		Symbol:    "BTCUSDT",
		Side:      types.Buy,
		Price:     decimal.NewFromInt(100),
		Quantity:  decimal.NewFromInt(5),
		Sequence:  1,
		Timestamp: 1700000000000,
	}
	if err := ob.ApplyDelta(d); err != nil {
		t.Fatalf("expected delta accepted, got: %v", err)
	}
	bids := ob.GetBids()
	if len(bids) != 1 {
		t.Fatalf("expected 1 bid level, got %d", len(bids))
	}
	if !bids[0].Price.Equal(decimal.NewFromInt(100)) {
		t.Errorf("expected price 100, got %s", bids[0].Price)
	}
	if !bids[0].Quantity.Equal(decimal.NewFromInt(5)) {
		t.Errorf("expected qty 5, got %s", bids[0].Quantity)
	}
}

// TestApplyDelta_HappyPath_InsertAsk verifies a fresh delta inserts an ask level.
func TestApplyDelta_HappyPath_InsertAsk(t *testing.T) {
	ob := newTestBook()
	d := &types.DepthDelta{
		Symbol:    "BTCUSDT",
		Side:      types.Sell,
		Price:     decimal.NewFromInt(101),
		Quantity:  decimal.NewFromInt(3),
		Sequence:  1,
		Timestamp: 1700000000000,
	}
	if err := ob.ApplyDelta(d); err != nil {
		t.Fatalf("expected delta accepted, got: %v", err)
	}
	asks := ob.GetAsks()
	if len(asks) != 1 {
		t.Fatalf("expected 1 ask level, got %d", len(asks))
	}
	if !asks[0].Price.Equal(decimal.NewFromInt(101)) {
		t.Errorf("expected price 101, got %s", asks[0].Price)
	}
}

// TestApplyDelta_UpdateExistingLevel verifies a delta for an existing price updates quantity.
func TestApplyDelta_UpdateExistingLevel(t *testing.T) {
	ob := newTestBook()
	mustApply(t, ob, &types.DepthDelta{
		Symbol: "BTCUSDT", Side: types.Buy,
		Price: decimal.NewFromInt(100), Quantity: decimal.NewFromInt(5),
		Sequence: 1, Timestamp: 1700000000000,
	})
	mustApply(t, ob, &types.DepthDelta{
		Symbol: "BTCUSDT", Side: types.Buy,
		Price: decimal.NewFromInt(100), Quantity: decimal.NewFromInt(7),
		Sequence: 2, Timestamp: 1700000000001,
	})
	bids := ob.GetBids()
	if len(bids) != 1 {
		t.Fatalf("expected 1 bid level, got %d", len(bids))
	}
	if !bids[0].Quantity.Equal(decimal.NewFromInt(7)) {
		t.Errorf("expected updated qty 7, got %s", bids[0].Quantity)
	}
}

// TestApplyDelta_ZeroQuantityRemovesLevel verifies qty=0 deletes the level.
func TestApplyDelta_ZeroQuantityRemovesLevel(t *testing.T) {
	ob := newTestBook()
	mustApply(t, ob, &types.DepthDelta{
		Symbol: "BTCUSDT", Side: types.Buy,
		Price: decimal.NewFromInt(100), Quantity: decimal.NewFromInt(5),
		Sequence: 1, Timestamp: 1700000000000,
	})
	mustApply(t, ob, &types.DepthDelta{
		Symbol: "BTCUSDT", Side: types.Buy,
		Price: decimal.NewFromInt(100), Quantity: decimal.Zero,
		Sequence: 2, Timestamp: 1700000000001,
	})
	if bids := ob.GetBids(); len(bids) != 0 {
		t.Fatalf("expected level removed, got %d bids", len(bids))
	}
}

// TestApplyDelta_RejectsSymbolMismatch verifies cross-symbol deltas are refused.
func TestApplyDelta_RejectsSymbolMismatch(t *testing.T) {
	ob := newTestBook()
	d := &types.DepthDelta{
		Symbol: "ETHUSDT", Side: types.Buy,
		Price: decimal.NewFromInt(100), Quantity: decimal.NewFromInt(5),
		Sequence: 1, Timestamp: 1700000000000,
	}
	if err := ob.ApplyDelta(d); err == nil {
		t.Fatal("expected symbol mismatch error, got nil")
	}
	if bids := ob.GetBids(); len(bids) != 0 {
		t.Errorf("book should be empty after rejected delta, got %d bids", len(bids))
	}
}

// TestApplyDelta_RejectsInvalidPayload verifies schema validation runs first.
func TestApplyDelta_RejectsInvalidPayload(t *testing.T) {
	ob := newTestBook()
	d := &types.DepthDelta{
		Symbol: "BTCUSDT", Side: types.Buy,
		Price: decimal.NewFromInt(-1), Quantity: decimal.NewFromInt(5),
		Sequence: 1, Timestamp: 1700000000000,
	}
	if err := ob.ApplyDelta(d); err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if bids := ob.GetBids(); len(bids) != 0 {
		t.Errorf("book should remain untouched on rejected delta")
	}
}

// TestApplyDelta_RejectsStaleSequence verifies a delta with sequence <= last applied is dropped.
func TestApplyDelta_RejectsStaleSequence(t *testing.T) {
	ob := newTestBook()
	mustApply(t, ob, &types.DepthDelta{
		Symbol: "BTCUSDT", Side: types.Buy,
		Price: decimal.NewFromInt(100), Quantity: decimal.NewFromInt(5),
		Sequence: 10, Timestamp: 1700000000010,
	})
	beforeBids := ob.GetBids()

	// Stale: sequence 10 again (duplicate / out-of-order older-or-equal).
	stale := &types.DepthDelta{
		Symbol: "BTCUSDT", Side: types.Buy,
		Price: decimal.NewFromInt(100), Quantity: decimal.NewFromInt(99),
		Sequence: 10, Timestamp: 1700000000011,
	}
	err := ob.ApplyDelta(stale)
	if err == nil {
		t.Fatal("expected stale-sequence error, got nil")
	}
	sge, ok := err.(*types.SequenceGapError)
	if !ok {
		t.Fatalf("expected *SequenceGapError, got %T: %v", err, err)
	}
	if !sge.IsStale() {
		t.Errorf("expected IsStale() true, got false (got=%d expected=%d)", sge.Got, sge.Expected)
	}

	// Book must be unchanged.
	afterBids := ob.GetBids()
	if len(beforeBids) != len(afterBids) {
		t.Fatalf("book mutated on stale delta: before=%d after=%d", len(beforeBids), len(afterBids))
	}
	if !beforeBids[0].Quantity.Equal(decimal.NewFromInt(5)) {
		t.Errorf("stale delta overwrote quantity: %s", afterBids[0].Quantity)
	}
}

// TestApplyDelta_RejectsSequenceGap verifies a gap > 1 is refused (book must be re-snapshotted).
func TestApplyDelta_RejectsSequenceGap(t *testing.T) {
	ob := newTestBook()
	mustApply(t, ob, &types.DepthDelta{
		Symbol: "BTCUSDT", Side: types.Buy,
		Price: decimal.NewFromInt(100), Quantity: decimal.NewFromInt(5),
		Sequence: 10, Timestamp: 1700000000010,
	})
	beforeBids := ob.GetBids()

	gap := &types.DepthDelta{
		Symbol: "BTCUSDT", Side: types.Buy,
		Price: decimal.NewFromInt(200), Quantity: decimal.NewFromInt(1),
		Sequence: 13, Timestamp: 1700000000013,
	}
	err := ob.ApplyDelta(gap)
	if err == nil {
		t.Fatal("expected sequence-gap error, got nil")
	}
	sge, ok := err.(*types.SequenceGapError)
	if !ok {
		t.Fatalf("expected *SequenceGapError, got %T", err)
	}
	if sge.IsStale() {
		t.Error("gap delta should not be classified as stale")
	}
	if sge.Gap() != 2 {
		t.Errorf("expected gap=2 missing sequences, got %d", sge.Gap())
	}

	// Book must be untouched.
	afterBids := ob.GetBids()
	if len(afterBids) != len(beforeBids) || !afterBids[0].Price.Equal(beforeBids[0].Price) {
		t.Errorf("book mutated on gap delta: before=%v after=%v", beforeBids, afterBids)
	}
}

// TestApplyDelta_SnapshotThenValidDelta exercises the canonical snapshot->delta flow.
func TestApplyDelta_SnapshotThenValidDelta(t *testing.T) {
	ob := newTestBook()

	// Snapshot: seed two bids + two asks.
	ob.ApplySnapshot(&types.DepthUpdate{
		Symbol: "BTCUSDT",
		Bids: []types.Level{
			{Price: decimal.NewFromInt(100), Quantity: decimal.NewFromInt(5)},
			{Price: decimal.NewFromInt(99), Quantity: decimal.NewFromInt(2)},
		},
		Asks: []types.Level{
			{Price: decimal.NewFromInt(101), Quantity: decimal.NewFromInt(4)},
			{Price: decimal.NewFromInt(102), Quantity: decimal.NewFromInt(6)},
		},
	})

	if bids := ob.GetBids(); len(bids) != 2 {
		t.Fatalf("snapshot bids expected 2, got %d", len(bids))
	}
	if asks := ob.GetAsks(); len(asks) != 2 {
		t.Fatalf("snapshot asks expected 2, got %d", len(asks))
	}

	// Now apply a valid delta with sequence 1 — must update one bid.
	if err := ob.ApplyDelta(&types.DepthDelta{
		Symbol: "BTCUSDT", Side: types.Buy,
		Price: decimal.NewFromInt(100), Quantity: decimal.NewFromInt(10),
		Sequence: 1, Timestamp: 1700000000001,
	}); err != nil {
		t.Fatalf("valid delta rejected: %v", err)
	}

	bids := ob.GetBids()
	if len(bids) != 2 {
		t.Fatalf("expected 2 bids after delta, got %d", len(bids))
	}
	// 100 should be top (highest bid).
	if !bids[0].Price.Equal(decimal.NewFromInt(100)) {
		t.Errorf("expected top bid 100, got %s", bids[0].Price)
	}
	if !bids[0].Quantity.Equal(decimal.NewFromInt(10)) {
		t.Errorf("expected updated qty 10, got %s", bids[0].Quantity)
	}
}

// TestApplyDelta_OnClosedBook verifies a closed book rejects further deltas.
func TestApplyDelta_OnClosedBook(t *testing.T) {
	ob := newTestBook()
	ob.Close()
	d := &types.DepthDelta{
		Symbol: "BTCUSDT", Side: types.Buy,
		Price: decimal.NewFromInt(100), Quantity: decimal.NewFromInt(5),
		Sequence: 1, Timestamp: 1700000000000,
	}
	if err := ob.ApplyDelta(d); err == nil {
		t.Fatal("expected ErrBookClosed, got nil")
	}
}

func mustApply(t *testing.T, ob *OrderBook, d *types.DepthDelta) {
	t.Helper()
	if err := ob.ApplyDelta(d); err != nil {
		t.Fatalf("ApplyDelta failed: %v", err)
	}
}
