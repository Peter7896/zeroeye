package orderbook

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/tent-of-trials/market/types"
)

func TestSnapshotThenValidDeltas(t *testing.T) {
	book := newTestBook(t)

	snapshot := &types.DepthUpdate{
		Symbol: "BTC-USD",
		Bids: []types.Level{
			level("99", "1"),
			level("98", "2"),
		},
		Asks: []types.Level{
			level("101", "1"),
			level("102", "2"),
		},
	}
	if err := book.ApplySnapshot(snapshot, 10); err != nil {
		t.Fatalf("ApplySnapshot() error = %v", err)
	}

	if err := book.ApplyDelta(Delta{
		Symbol:   "BTC-USD",
		Sequence: 11,
		Side:     DeltaBid,
		Price:    dec("100"),
		Quantity: dec("3"),
	}); err != nil {
		t.Fatalf("ApplyDelta(bid) error = %v", err)
	}
	if err := book.ApplyDelta(Delta{
		Symbol:   "BTC-USD",
		Sequence: 12,
		Side:     DeltaAsk,
		Price:    dec("101"),
		Quantity: dec("0"),
	}); err != nil {
		t.Fatalf("ApplyDelta(ask removal) error = %v", err)
	}

	bids := book.GetBids()
	if len(bids) != 3 || !bids[0].Price.Equal(dec("100")) || !bids[0].Quantity.Equal(dec("3")) {
		t.Fatalf("unexpected bids after deltas: %#v", bids)
	}
	asks := book.GetAsks()
	if len(asks) != 1 || !asks[0].Price.Equal(dec("102")) {
		t.Fatalf("unexpected asks after deltas: %#v", asks)
	}
	if got := book.Sequence(); got != 12 {
		t.Fatalf("Sequence() = %d, want 12", got)
	}
}

func TestApplyDeltaRejectsMalformedPayloadsWithoutMutation(t *testing.T) {
	tests := []struct {
		name  string
		delta Delta
		want  error
	}{
		{
			name: "wrong symbol",
			delta: Delta{
				Symbol:   "ETH-USD",
				Sequence: 11,
				Side:     DeltaBid,
				Price:    dec("100"),
				Quantity: dec("1"),
			},
			want: ErrInvalidSymbol,
		},
		{
			name: "invalid side",
			delta: Delta{
				Symbol:   "BTC-USD",
				Sequence: 11,
				Side:     "middle",
				Price:    dec("100"),
				Quantity: dec("1"),
			},
			want: ErrInvalidSide,
		},
		{
			name: "invalid price",
			delta: Delta{
				Symbol:   "BTC-USD",
				Sequence: 11,
				Side:     DeltaBid,
				Price:    dec("0"),
				Quantity: dec("1"),
			},
			want: ErrInvalidPrice,
		},
		{
			name: "invalid quantity",
			delta: Delta{
				Symbol:   "BTC-USD",
				Sequence: 11,
				Side:     DeltaAsk,
				Price:    dec("101"),
				Quantity: dec("-1"),
			},
			want: ErrInvalidQuantity,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			book := seededBook(t)
			before := book.Checksum()
			beforeSequence := book.Sequence()

			if err := book.ApplyDelta(tt.delta); err != tt.want {
				t.Fatalf("ApplyDelta() error = %v, want %v", err, tt.want)
			}
			assertStatePreserved(t, book, before, beforeSequence)
		})
	}
}

func TestApplyDeltaRejectsStaleOrOutOfOrderWithoutMutation(t *testing.T) {
	book := seededBook(t)
	before := book.Checksum()
	beforeSequence := book.Sequence()

	if err := book.ApplyDelta(Delta{
		Symbol:   "BTC-USD",
		Sequence: beforeSequence,
		Side:     DeltaBid,
		Price:    dec("100"),
		Quantity: dec("9"),
	}); err != ErrStaleDelta {
		t.Fatalf("ApplyDelta(stale) error = %v, want %v", err, ErrStaleDelta)
	}
	assertStatePreserved(t, book, before, beforeSequence)

	if err := book.ApplyDelta(Delta{
		Symbol:   "BTC-USD",
		Sequence: beforeSequence - 1,
		Side:     DeltaAsk,
		Price:    dec("101"),
		Quantity: dec("0"),
	}); err != ErrStaleDelta {
		t.Fatalf("ApplyDelta(out-of-order) error = %v, want %v", err, ErrStaleDelta)
	}
	assertStatePreserved(t, book, before, beforeSequence)
}

func TestApplyDeltaRejectsChecksumMismatchWithoutMutation(t *testing.T) {
	book := seededBook(t)
	before := book.Checksum()
	beforeSequence := book.Sequence()

	if err := book.ApplyDelta(Delta{
		Symbol:   "BTC-USD",
		Sequence: beforeSequence + 1,
		Side:     DeltaBid,
		Price:    dec("100"),
		Quantity: dec("9"),
		Checksum: "not-the-post-delta-checksum",
	}); err != ErrChecksumMismatch {
		t.Fatalf("ApplyDelta(checksum mismatch) error = %v, want %v", err, ErrChecksumMismatch)
	}
	assertStatePreserved(t, book, before, beforeSequence)
}

func TestApplySnapshotRejectsMalformedLevelsWithoutMutation(t *testing.T) {
	book := seededBook(t)
	before := book.Checksum()
	beforeSequence := book.Sequence()

	if err := book.ApplySnapshot(&types.DepthUpdate{
		Symbol: "BTC-USD",
		Bids: []types.Level{
			level("-1", "1"),
		},
	}, beforeSequence+1); err != ErrInvalidPrice {
		t.Fatalf("ApplySnapshot() error = %v, want %v", err, ErrInvalidPrice)
	}

	assertStatePreserved(t, book, before, beforeSequence)
}

func seededBook(t *testing.T) *OrderBook {
	t.Helper()
	book := newTestBook(t)
	if err := book.ApplySnapshot(&types.DepthUpdate{
		Symbol: "BTC-USD",
		Bids: []types.Level{
			level("99", "1"),
		},
		Asks: []types.Level{
			level("101", "2"),
		},
	}, 10); err != nil {
		t.Fatalf("ApplySnapshot() error = %v", err)
	}
	return book
}

func newTestBook(t *testing.T) *OrderBook {
	t.Helper()
	return NewOrderBook("BTC-USD", Config{MaxDepth: 100})
}

func level(price string, quantity string) types.Level {
	return types.Level{
		Price:    dec(price),
		Quantity: dec(quantity),
		Count:    1,
	}
}

func dec(value string) decimal.Decimal {
	return decimal.RequireFromString(value)
}

func assertStatePreserved(t *testing.T, book *OrderBook, wantChecksum string, wantSequence uint64) {
	t.Helper()
	if got := book.Checksum(); got != wantChecksum {
		t.Fatalf("Checksum() = %q, want preserved %q", got, wantChecksum)
	}
	if got := book.Sequence(); got != wantSequence {
		t.Fatalf("Sequence() = %d, want preserved %d", got, wantSequence)
	}
}
