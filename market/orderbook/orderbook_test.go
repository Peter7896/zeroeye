package orderbook

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/tent-of-trials/market/types"
)

// ---------------------------------------------------------------------------
// Delta validation — malformed payloads
// ---------------------------------------------------------------------------

func TestValidateDelta_MalformedPrice_Negative(t *testing.T) {
	delta := Delta{
		Symbol:   "BTC-USD",
		Side:     types.Buy,
		Price:    decimal.NewFromInt(-100),
		Quantity: decimal.NewFromInt(10),
		Sequence: 1,
	}
	err := ValidateDelta(delta, "BTC-USD", 0)
	if err != ErrInvalidPrice {
		t.Fatalf("expected ErrInvalidPrice, got %v", err)
	}
}

func TestValidateDelta_MalformedPrice_Zero(t *testing.T) {
	delta := Delta{
		Symbol:   "BTC-USD",
		Side:     types.Buy,
		Price:    decimal.Zero,
		Quantity: decimal.NewFromInt(10),
		Sequence: 1,
	}
	err := ValidateDelta(delta, "BTC-USD", 0)
	if err != ErrInvalidPrice {
		t.Fatalf("expected ErrInvalidPrice for zero price, got %v", err)
	}
}

func TestValidateDelta_MalformedQuantity_Negative(t *testing.T) {
	delta := Delta{
		Symbol:   "ETH-USD",
		Side:     types.Sell,
		Price:    decimal.NewFromInt(2000),
		Quantity: decimal.NewFromInt(-5),
		Sequence: 1,
	}
	err := ValidateDelta(delta, "ETH-USD", 0)
	if err != ErrInvalidQuantity {
		t.Fatalf("expected ErrInvalidQuantity, got %v", err)
	}
}

func TestValidateDelta_MalformedSide_OutOfRange(t *testing.T) {
	delta := Delta{
		Symbol:   "BTC-USD",
		Side:     types.OrderSide(99),
		Price:    decimal.NewFromInt(50000),
		Quantity: decimal.NewFromInt(1),
		Sequence: 1,
	}
	err := ValidateDelta(delta, "BTC-USD", 0)
	if err != ErrInvalidSide {
		t.Fatalf("expected ErrInvalidSide for out-of-range side, got %v", err)
	}
}

func TestValidateDelta_SymbolMismatch(t *testing.T) {
	delta := Delta{
		Symbol:   "ETH-USD",
		Side:     types.Buy,
		Price:    decimal.NewFromInt(2000),
		Quantity: decimal.NewFromInt(1),
		Sequence: 1,
	}
	err := ValidateDelta(delta, "BTC-USD", 0)
	if err != ErrSymbolMismatch {
		t.Fatalf("expected ErrSymbolMismatch, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Sequence validation — stale / out-of-order
// ---------------------------------------------------------------------------

func TestValidateDelta_StaleSequence_Equal(t *testing.T) {
	delta := Delta{
		Symbol:   "BTC-USD",
		Side:     types.Buy,
		Price:    decimal.NewFromInt(50000),
		Quantity: decimal.NewFromInt(1),
		Sequence: 5,
	}
	err := ValidateDelta(delta, "BTC-USD", 5)
	if err != ErrStaleSequence {
		t.Fatalf("expected ErrStaleSequence for equal sequence, got %v", err)
	}
}

func TestValidateDelta_StaleSequence_Less(t *testing.T) {
	delta := Delta{
		Symbol:   "BTC-USD",
		Side:     types.Buy,
		Price:    decimal.NewFromInt(50000),
		Quantity: decimal.NewFromInt(1),
		Sequence: 3,
	}
	err := ValidateDelta(delta, "BTC-USD", 5)
	if err != ErrStaleSequence {
		t.Fatalf("expected ErrStaleSequence for lower sequence, got %v", err)
	}
}

func TestValidateDelta_ValidSequence_Increment(t *testing.T) {
	delta := Delta{
		Symbol:   "BTC-USD",
		Side:     types.Sell,
		Price:    decimal.NewFromInt(51000),
		Quantity: decimal.NewFromInt(2),
		Sequence: 6,
	}
	err := ValidateDelta(delta, "BTC-USD", 5)
	if err != nil {
		t.Fatalf("expected no error for valid sequence, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// ApplyDelta — full cycle (validate + mutate + verify)
// ---------------------------------------------------------------------------

func TestApplyDelta_ValidAfterSnapshot_PreservesOrdering(t *testing.T) {
	ob := NewOrderBook("BTC-USD", Config{MaxDepth: 100, PriceDecimals: 2, VolumeDecimals: 8})

	// 1. Apply snapshot
	snap := &types.DepthUpdate{
		Symbol: "BTC-USD",
		Bids: []types.Level{
			{Price: decimal.NewFromInt(50000), Quantity: decimal.NewFromInt(2), Count: 1},
			{Price: decimal.NewFromInt(49900), Quantity: decimal.NewFromInt(3), Count: 1},
		},
		Asks: []types.Level{
			{Price: decimal.NewFromInt(50100), Quantity: decimal.NewFromInt(1), Count: 1},
			{Price: decimal.NewFromInt(50200), Quantity: decimal.NewFromInt(4), Count: 1},
		},
		Timestamp: 1000000,
	}
	if err := ob.ApplySnapshot(snap); err != nil {
		t.Fatalf("ApplySnapshot failed: %v", err)
	}

	// 2. Apply valid deltas
	deltas := []Delta{
		{Symbol: "BTC-USD", Side: types.Buy, Price: decimal.NewFromInt(50000), Quantity: decimal.NewFromInt(5), Sequence: 1},
		{Symbol: "BTC-USD", Side: types.Sell, Price: decimal.NewFromInt(50100), Quantity: decimal.NewFromInt(0), Sequence: 2},
		{Symbol: "BTC-USD", Side: types.Buy, Price: decimal.NewFromInt(49800), Quantity: decimal.NewFromInt(10), Sequence: 3},
	}
	for i, d := range deltas {
		if err := ob.ApplyDelta(d); err != nil {
			t.Fatalf("delta %d failed: %v", i, err)
		}
	}

	// Verify bids
	bids := ob.GetBids()
	if len(bids) != 3 {
		t.Fatalf("expected 3 bids, got %d", len(bids))
	}
	// Bids should be sorted descending
	if !bids[0].Price.Equal(decimal.NewFromInt(50000)) {
		t.Errorf("bid[0] price = %s, want 50000", bids[0].Price)
	}
	if !bids[0].Quantity.Equal(decimal.NewFromInt(5)) {
		t.Errorf("bid[0] qty = %s, want 5 (upserted)", bids[0].Quantity)
	}
	if !bids[1].Price.Equal(decimal.NewFromInt(49900)) {
		t.Errorf("bid[1] price = %s, want 49900", bids[1].Price)
	}
	if !bids[2].Price.Equal(decimal.NewFromInt(49800)) {
		t.Errorf("bid[2] price = %s, want 49800", bids[2].Price)
	}

	// Verify asks (50100 was removed by zero-quantity delta)
	asks := ob.GetAsks()
	if len(asks) != 1 {
		t.Fatalf("expected 1 ask, got %d", len(asks))
	}
	if !asks[0].Price.Equal(decimal.NewFromInt(50200)) {
		t.Errorf("ask[0] price = %s, want 50200", asks[0].Price)
	}
}

// ---------------------------------------------------------------------------
// State preservation — book unchanged after invalid delta
// ---------------------------------------------------------------------------

func TestApplyDelta_StatePreserved_OnInvalidDelta(t *testing.T) {
	ob := NewOrderBook("BTC-USD", Config{MaxDepth: 100, PriceDecimals: 2, VolumeDecimals: 8})

	snap := &types.DepthUpdate{
		Symbol: "BTC-USD",
		Bids: []types.Level{
			{Price: decimal.NewFromInt(50000), Quantity: decimal.NewFromInt(3), Count: 1},
		},
		Asks: []types.Level{
			{Price: decimal.NewFromInt(50100), Quantity: decimal.NewFromInt(2), Count: 1},
		},
		Timestamp: 1000000,
	}
	if err := ob.ApplySnapshot(snap); err != nil {
		t.Fatalf("ApplySnapshot failed: %v", err)
	}

	// Capture state before bad delta
	bidsBefore := ob.GetBids()
	asksBefore := ob.GetAsks()
	seqBefore := ob.GetSequence()

	// Try a malformed delta
	badDelta := Delta{
		Symbol:   "ETH-USD", // wrong symbol
		Side:     types.Buy,
		Price:    decimal.NewFromInt(50000),
		Quantity: decimal.NewFromInt(1),
		Sequence: 1,
	}
	err := ob.ApplyDelta(badDelta)
	if err == nil {
		t.Fatal("expected error for wrong symbol, got nil")
	}

	// Verify state unchanged
	bidsAfter := ob.GetBids()
	asksAfter := ob.GetAsks()
	seqAfter := ob.GetSequence()

	if len(bidsAfter) != len(bidsBefore) {
		t.Errorf("bids changed: before=%d after=%d", len(bidsBefore), len(bidsAfter))
	}
	if len(asksAfter) != len(asksBefore) {
		t.Errorf("asks changed: before=%d after=%d", len(asksBefore), len(asksAfter))
	}
	if seqAfter != seqBefore {
		t.Errorf("sequence changed: before=%d after=%d", seqBefore, seqAfter)
	}
}

func TestApplyDelta_StatePreserved_OnStaleSequence(t *testing.T) {
	ob := NewOrderBook("BTC-USD", Config{MaxDepth: 100, PriceDecimals: 2, VolumeDecimals: 8})

	snap := &types.DepthUpdate{
		Symbol: "BTC-USD",
		Bids: []types.Level{
			{Price: decimal.NewFromInt(50000), Quantity: decimal.NewFromInt(3), Count: 1},
		},
		Asks:  []types.Level{},
		Timestamp: 1000000,
	}
	ob.ApplySnapshot(snap)

	// Apply one valid delta to bump sequence
	ob.ApplyDelta(Delta{
		Symbol: "BTC-USD", Side: types.Buy,
		Price: decimal.NewFromInt(49900), Quantity: decimal.NewFromInt(1), Sequence: 1,
	})

	bidsBefore := ob.GetBids()
	seqBefore := ob.GetSequence()

	// Try stale sequence
	stale := Delta{
		Symbol: "BTC-USD", Side: types.Buy,
		Price: decimal.NewFromInt(49800), Quantity: decimal.NewFromInt(5), Sequence: 1,
	}
	err := ob.ApplyDelta(stale)
	if err != ErrStaleSequence {
		t.Fatalf("expected ErrStaleSequence, got %v", err)
	}

	bidsAfter := ob.GetBids()
	seqAfter := ob.GetSequence()

	if len(bidsAfter) != len(bidsBefore) {
		t.Errorf("bids mutated after stale delta")
	}
	if seqAfter != seqBefore {
		t.Errorf("sequence changed: before=%d after=%d", seqBefore, seqAfter)
	}
}

func TestApplyDelta_StatePreserved_OnMalformedPrice(t *testing.T) {
	ob := NewOrderBook("BTC-USD", Config{MaxDepth: 100, PriceDecimals: 2, VolumeDecimals: 8})

	snap := &types.DepthUpdate{
		Symbol: "BTC-USD",
		Bids: []types.Level{
			{Price: decimal.NewFromInt(50000), Quantity: decimal.NewFromInt(3), Count: 1},
		},
		Asks:  []types.Level{},
		Timestamp: 1000000,
	}
	ob.ApplySnapshot(snap)

	bidsBefore := ob.GetBids()

	bad := Delta{
		Symbol: "BTC-USD", Side: types.Buy,
		Price: decimal.NewFromInt(-1), Quantity: decimal.NewFromInt(1), Sequence: 1,
	}
	err := ob.ApplyDelta(bad)
	if err != ErrInvalidPrice {
		t.Fatalf("expected ErrInvalidPrice, got %v", err)
	}

	bidsAfter := ob.GetBids()
	if len(bidsAfter) != len(bidsBefore) {
		t.Errorf("bids mutated after bad price delta")
	}
}

func TestApplyDelta_StatePreserved_OnMalformedQuantity(t *testing.T) {
	ob := NewOrderBook("BTC-USD", Config{MaxDepth: 100, PriceDecimals: 2, VolumeDecimals: 8})

	snap := &types.DepthUpdate{
		Symbol: "BTC-USD",
		Bids: []types.Level{
			{Price: decimal.NewFromInt(50000), Quantity: decimal.NewFromInt(3), Count: 1},
		},
		Asks:  []types.Level{},
		Timestamp: 1000000,
	}
	ob.ApplySnapshot(snap)

	bidsBefore := ob.GetBids()

	bad := Delta{
		Symbol: "BTC-USD", Side: types.Buy,
		Price: decimal.NewFromInt(50100), Quantity: decimal.NewFromInt(-5), Sequence: 1,
	}
	err := ob.ApplyDelta(bad)
	if err != ErrInvalidQuantity {
		t.Fatalf("expected ErrInvalidQuantity, got %v", err)
	}

	bidsAfter := ob.GetBids()
	if len(bidsAfter) != len(bidsBefore) {
		t.Errorf("bids mutated after bad quantity delta")
	}
}

// ---------------------------------------------------------------------------
// Checksum-style mismatch — invalid snapshot preserves previous state
// ---------------------------------------------------------------------------

func TestApplySnapshot_SymbolMismatch_PreservesState(t *testing.T) {
	ob := NewOrderBook("BTC-USD", Config{MaxDepth: 100, PriceDecimals: 2, VolumeDecimals: 8})

	// Seed with valid snapshot
	snap1 := &types.DepthUpdate{
		Symbol: "BTC-USD",
		Bids: []types.Level{
			{Price: decimal.NewFromInt(50000), Quantity: decimal.NewFromInt(3), Count: 1},
		},
		Asks: []types.Level{
			{Price: decimal.NewFromInt(50100), Quantity: decimal.NewFromInt(2), Count: 1},
		},
		Timestamp: 1000000,
	}
	if err := ob.ApplySnapshot(snap1); err != nil {
		t.Fatalf("first ApplySnapshot failed: %v", err)
	}

	bidsBefore := ob.GetBids()
	asksBefore := ob.GetAsks()

	// Try to apply snapshot for wrong symbol
	snap2 := &types.DepthUpdate{
		Symbol: "ETH-USD",
		Bids: []types.Level{
			{Price: decimal.NewFromInt(2000), Quantity: decimal.NewFromInt(10), Count: 1},
		},
		Asks:  []types.Level{},
		Timestamp: 2000000,
	}
	err := ob.ApplySnapshot(snap2)
	if err != ErrSymbolMismatch {
		t.Fatalf("expected ErrSymbolMismatch, got %v", err)
	}

	bidsAfter := ob.GetBids()
	asksAfter := ob.GetAsks()

	if len(bidsAfter) != len(bidsBefore) {
		t.Errorf("bids changed after bad snapshot: before=%d after=%d", len(bidsBefore), len(bidsAfter))
	}
	if len(asksAfter) != len(asksBefore) {
		t.Errorf("asks changed after bad snapshot: before=%d after=%d", len(asksBefore), len(asksAfter))
	}
}

func TestApplySnapshot_NegativePrice_PreservesState(t *testing.T) {
	ob := NewOrderBook("BTC-USD", Config{MaxDepth: 100, PriceDecimals: 2, VolumeDecimals: 8})

	snap1 := &types.DepthUpdate{
		Symbol: "BTC-USD",
		Bids: []types.Level{
			{Price: decimal.NewFromInt(50000), Quantity: decimal.NewFromInt(3), Count: 1},
		},
		Asks:  []types.Level{},
		Timestamp: 1000000,
	}
	ob.ApplySnapshot(snap1)

	bidsBefore := ob.GetBids()

	snap2 := &types.DepthUpdate{
		Symbol: "BTC-USD",
		Bids: []types.Level{
			{Price: decimal.NewFromInt(-100), Quantity: decimal.NewFromInt(1), Count: 1},
		},
		Asks:  []types.Level{},
		Timestamp: 2000000,
	}
	err := ob.ApplySnapshot(snap2)
	if err != ErrInvalidPrice {
		t.Fatalf("expected ErrInvalidPrice, got %v", err)
	}

	bidsAfter := ob.GetBids()
	if len(bidsAfter) != len(bidsBefore) {
		t.Errorf("bids changed after bad snapshot")
	}
}

// ---------------------------------------------------------------------------
// Snapshot + delta recovery path
// ---------------------------------------------------------------------------

func TestSnapshotThenDeltas_RecoveryPath(t *testing.T) {
	ob := NewOrderBook("BTC-USD", Config{MaxDepth: 100, PriceDecimals: 2, VolumeDecimals: 8})

	// Initial snapshot
	snap := &types.DepthUpdate{
		Symbol: "BTC-USD",
		Bids: []types.Level{
			{Price: decimal.NewFromInt(50000), Quantity: decimal.NewFromInt(3), Count: 1},
			{Price: decimal.NewFromInt(49900), Quantity: decimal.NewFromInt(5), Count: 1},
		},
		Asks: []types.Level{
			{Price: decimal.NewFromInt(50100), Quantity: decimal.NewFromInt(2), Count: 1},
		},
		Timestamp: 1000000,
	}
	if err := ob.ApplySnapshot(snap); err != nil {
		t.Fatalf("ApplySnapshot failed: %v", err)
	}

	// Sequence should reset to 0 after snapshot
	if seq := ob.GetSequence(); seq != 0 {
		t.Fatalf("expected sequence 0 after snapshot, got %d", seq)
	}

	// Valid delta sequence
	d1 := Delta{
		Symbol: "BTC-USD", Side: types.Buy,
		Price: decimal.NewFromInt(50000), Quantity: decimal.NewFromInt(10), Sequence: 1,
	}
	if err := ob.ApplyDelta(d1); err != nil {
		t.Fatalf("delta 1 failed: %v", err)
	}

	// Another valid delta
	d2 := Delta{
		Symbol: "BTC-USD", Side: types.Sell,
		Price: decimal.NewFromInt(50200), Quantity: decimal.NewFromInt(4), Sequence: 2,
	}
	if err := ob.ApplyDelta(d2); err != nil {
		t.Fatalf("delta 2 failed: %v", err)
	}

	// Verify final state
	bids := ob.GetBids()
	if len(bids) != 2 {
		t.Fatalf("expected 2 bids, got %d", len(bids))
	}
	if !bids[0].Quantity.Equal(decimal.NewFromInt(10)) {
		t.Errorf("bid[0] qty = %s, want 10", bids[0].Quantity)
	}

	asks := ob.GetAsks()
	if len(asks) != 2 {
		t.Fatalf("expected 2 asks, got %d", len(asks))
	}

	if seq := ob.GetSequence(); seq != 2 {
		t.Errorf("expected sequence 2, got %d", seq)
	}
}

// ---------------------------------------------------------------------------
// Sequence tracking
// ---------------------------------------------------------------------------

func TestSequence_IncrementsWithEachAddOrder(t *testing.T) {
	ob := NewOrderBook("BTC-USD", Config{MaxDepth: 100})

	seq0 := ob.GetSequence()
	if seq0 != 0 {
		t.Fatalf("expected initial sequence 0, got %d", seq0)
	}

	ob.AddOrder(&types.Order{
		Symbol:       "BTC-USD",
		Side:         types.Buy,
		Type:         types.Limit,
		Price:        decimal.NewFromInt(50000),
		Quantity:     decimal.NewFromInt(1),
		RemainingQty: decimal.NewFromInt(1),
	})
	if seq := ob.GetSequence(); seq != 1 {
		t.Errorf("expected sequence 1 after first order, got %d", seq)
	}

	ob.AddOrder(&types.Order{
		Symbol:       "BTC-USD",
		Side:         types.Sell,
		Type:         types.Limit,
		Price:        decimal.NewFromInt(51000),
		Quantity:     decimal.NewFromInt(2),
		RemainingQty: decimal.NewFromInt(2),
	})
	if seq := ob.GetSequence(); seq != 2 {
		t.Errorf("expected sequence 2 after second order, got %d", seq)
	}
}

// ---------------------------------------------------------------------------
// Closed book rejection
// ---------------------------------------------------------------------------

func TestApplyDelta_ClosedBook(t *testing.T) {
	ob := NewOrderBook("BTC-USD", Config{MaxDepth: 100})
	ob.Close()

	err := ob.ApplyDelta(Delta{
		Symbol: "BTC-USD", Side: types.Buy,
		Price: decimal.NewFromInt(50000), Quantity: decimal.NewFromInt(1), Sequence: 1,
	})
	if err != ErrBookClosed {
		t.Fatalf("expected ErrBookClosed, got %v", err)
	}
}

func TestApplySnapshot_ClosedBook(t *testing.T) {
	ob := NewOrderBook("BTC-USD", Config{MaxDepth: 100})
	ob.Close()

	err := ob.ApplySnapshot(&types.DepthUpdate{
		Symbol: "BTC-USD",
		Bids:   []types.Level{{Price: decimal.NewFromInt(50000), Quantity: decimal.NewFromInt(1), Count: 1}},
		Asks:   []types.Level{},
	})
	if err != ErrBookClosed {
		t.Fatalf("expected ErrBookClosed, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Valid deltas — happy path edge cases
// ---------------------------------------------------------------------------

func TestApplyDelta_ZeroQuantity_RemovesLevel(t *testing.T) {
	ob := NewOrderBook("BTC-USD", Config{MaxDepth: 100})

	snap := &types.DepthUpdate{
		Symbol: "BTC-USD",
		Bids: []types.Level{
			{Price: decimal.NewFromInt(50000), Quantity: decimal.NewFromInt(3), Count: 1},
			{Price: decimal.NewFromInt(49900), Quantity: decimal.NewFromInt(5), Count: 1},
		},
		Asks:  []types.Level{},
		Timestamp: 1000000,
	}
	ob.ApplySnapshot(snap)

	// Remove the 50000 level
	d := Delta{
		Symbol: "BTC-USD", Side: types.Buy,
		Price: decimal.NewFromInt(50000), Quantity: decimal.Zero, Sequence: 1,
	}
	if err := ob.ApplyDelta(d); err != nil {
		t.Fatalf("zero-quantity delta failed: %v", err)
	}

	bids := ob.GetBids()
	if len(bids) != 1 {
		t.Fatalf("expected 1 bid after removal, got %d", len(bids))
	}
	if !bids[0].Price.Equal(decimal.NewFromInt(49900)) {
		t.Errorf("remaining bid price = %s, want 49900", bids[0].Price)
	}
}

func TestApplyDelta_NewPriceLevel_InsertedSorted(t *testing.T) {
	ob := NewOrderBook("BTC-USD", Config{MaxDepth: 100})

	snap := &types.DepthUpdate{
		Symbol: "BTC-USD",
		Bids: []types.Level{
			{Price: decimal.NewFromInt(50000), Quantity: decimal.NewFromInt(3), Count: 1},
			{Price: decimal.NewFromInt(49800), Quantity: decimal.NewFromInt(2), Count: 1},
		},
		Asks:  []types.Level{},
		Timestamp: 1000000,
	}
	ob.ApplySnapshot(snap)

	// Insert between 50000 and 49800
	d := Delta{
		Symbol: "BTC-USD", Side: types.Buy,
		Price: decimal.NewFromInt(49900), Quantity: decimal.NewFromInt(5), Sequence: 1,
	}
	if err := ob.ApplyDelta(d); err != nil {
		t.Fatalf("insert delta failed: %v", err)
	}

	bids := ob.GetBids()
	if len(bids) != 3 {
		t.Fatalf("expected 3 bids, got %d", len(bids))
	}
	if !bids[0].Price.Equal(decimal.NewFromInt(50000)) {
		t.Errorf("bid[0] = %s, want 50000", bids[0].Price)
	}
	if !bids[1].Price.Equal(decimal.NewFromInt(49900)) {
		t.Errorf("bid[1] = %s, want 49900", bids[1].Price)
	}
	if !bids[2].Price.Equal(decimal.NewFromInt(49800)) {
		t.Errorf("bid[2] = %s, want 49800", bids[2].Price)
	}
}

func TestDeltaQuantity_ZeroIsAllowed_Removal(t *testing.T) {
	// Zero quantity for existing price = removal (valid)
	err := ValidateDelta(
		Delta{Symbol: "BTC-USD", Side: types.Buy, Price: decimal.NewFromInt(50000), Quantity: decimal.Zero, Sequence: 1},
		"BTC-USD", 0,
	)
	if err != nil {
		t.Fatalf("zero quantity should be valid (price removal), got %v", err)
	}
}
