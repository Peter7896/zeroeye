package orderbook

import (
	"errors"
	"fmt"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/tent-of-trials/market/types"
)

func TestApplySnapshotThenValidDelta(t *testing.T) {
	book := newTestBook(t)

	initial := types.DepthUpdate{
		Symbol: types.Symbol("BTC-USD"),
		Bids: []types.Level{
			level("100.00", "1.00", 1),
		},
		Asks: []types.Level{
			level("101.00", "1.00", 1),
		},
	}
	if err := book.ApplySnapshot(initial, 10); err != nil {
		t.Fatalf("ApplySnapshot() error = %v", err)
	}

	wantBids := []types.Level{
		level("100.00", "2.25", 3),
		level("99.50", "1.40", 1),
	}
	wantAsks := []types.Level{
		level("102.00", "0.75", 1),
	}

	delta := DepthDelta{
		Symbol:           types.Symbol("BTC-USD"),
		PreviousSequence: 10,
		Sequence:         11,
		Updates: []DepthDeltaLevel{
			{Side: "bid", Price: dec("100.00"), Quantity: dec("2.25"), Count: 3},
			{Side: "bid", Price: dec("99.50"), Quantity: dec("1.40")},
			{Side: "ask", Price: dec("101.00"), Quantity: decimal.Zero},
			{Side: "ask", Price: dec("102.00"), Quantity: dec("0.75")},
		},
		Checksum: DepthChecksum(wantBids, wantAsks),
	}

	if err := book.ApplyDelta(delta); err != nil {
		t.Fatalf("ApplyDelta() error = %v", err)
	}

	got := book.GetSnapshot()
	assertLevels(t, got.Bids, wantBids)
	assertLevels(t, got.Asks, wantAsks)
	if book.sequence != 11 {
		t.Fatalf("sequence = %d, want 11", book.sequence)
	}
}

func TestApplyDeltaRejectsMalformedPayloadsWithoutMutation(t *testing.T) {
	tests := []struct {
		name  string
		delta DepthDelta
		want  error
	}{
		{
			name:  "wrong symbol",
			delta: validDelta(11, 10, DepthDeltaLevel{Side: "bid", Price: dec("100.00"), Quantity: dec("2.00")}),
			want:  ErrInvalidDeltaSymbol,
		},
		{
			name: "stale sequence",
			delta: DepthDelta{
				Symbol:           types.Symbol("BTC-USD"),
				PreviousSequence: 9,
				Sequence:         10,
				Updates:          []DepthDeltaLevel{{Side: "bid", Price: dec("100.00"), Quantity: dec("2.00")}},
			},
			want: ErrInvalidDeltaSequence,
		},
		{
			name: "out of order previous sequence",
			delta: DepthDelta{
				Symbol:           types.Symbol("BTC-USD"),
				PreviousSequence: 8,
				Sequence:         11,
				Updates:          []DepthDeltaLevel{{Side: "bid", Price: dec("100.00"), Quantity: dec("2.00")}},
			},
			want: ErrInvalidDeltaSequence,
		},
		{
			name:  "invalid side",
			delta: validDelta(11, 10, DepthDeltaLevel{Side: "middle", Price: dec("100.00"), Quantity: dec("2.00")}),
			want:  ErrInvalidDeltaSide,
		},
		{
			name:  "invalid bid price",
			delta: validDelta(11, 10, DepthDeltaLevel{Side: "bid", Price: decimal.Zero, Quantity: dec("2.00")}),
			want:  ErrInvalidDeltaPrice,
		},
		{
			name:  "invalid ask quantity",
			delta: validDelta(11, 10, DepthDeltaLevel{Side: "ask", Price: dec("101.00"), Quantity: dec("-1.00")}),
			want:  ErrInvalidDeltaQuantity,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			book := seededBook(t)
			before := snapshotKey(book.GetSnapshot())

			if tt.name == "wrong symbol" {
				tt.delta.Symbol = types.Symbol("ETH-USD")
			}

			err := book.ApplyDelta(tt.delta)
			if !errors.Is(err, tt.want) {
				t.Fatalf("ApplyDelta() error = %v, want %v", err, tt.want)
			}
			if got := snapshotKey(book.GetSnapshot()); got != before {
				t.Fatalf("book mutated after invalid delta:\nbefore %s\nafter  %s", before, got)
			}
			if book.sequence != 10 {
				t.Fatalf("sequence mutated to %d, want 10", book.sequence)
			}
		})
	}
}

func TestApplyDeltaRejectsChecksumMismatchWithoutMutation(t *testing.T) {
	book := seededBook(t)
	before := snapshotKey(book.GetSnapshot())

	delta := validDelta(11, 10, DepthDeltaLevel{
		Side:     "ask",
		Price:    dec("102.00"),
		Quantity: dec("0.75"),
	})
	delta.Checksum = "not-the-calculated-book-checksum"

	err := book.ApplyDelta(delta)
	if !errors.Is(err, ErrDeltaChecksumMismatch) {
		t.Fatalf("ApplyDelta() error = %v, want %v", err, ErrDeltaChecksumMismatch)
	}
	if got := snapshotKey(book.GetSnapshot()); got != before {
		t.Fatalf("book mutated after checksum mismatch:\nbefore %s\nafter  %s", before, got)
	}
	if book.sequence != 10 {
		t.Fatalf("sequence mutated to %d, want 10", book.sequence)
	}
}

func newTestBook(t *testing.T) *OrderBook {
	t.Helper()
	return NewOrderBook(types.Symbol("BTC-USD"), Config{
		MaxDepth:       5,
		PriceDecimals:  2,
		VolumeDecimals: 8,
	})
}

func seededBook(t *testing.T) *OrderBook {
	t.Helper()
	book := newTestBook(t)
	err := book.ApplySnapshot(types.DepthUpdate{
		Symbol: types.Symbol("BTC-USD"),
		Bids:   []types.Level{level("100.00", "1.00", 1)},
		Asks:   []types.Level{level("101.00", "1.00", 1)},
	}, 10)
	if err != nil {
		t.Fatalf("ApplySnapshot() error = %v", err)
	}
	return book
}

func validDelta(sequence, previous uint64, updates ...DepthDeltaLevel) DepthDelta {
	return DepthDelta{
		Symbol:           types.Symbol("BTC-USD"),
		PreviousSequence: previous,
		Sequence:         sequence,
		Updates:          updates,
	}
}

func level(price, quantity string, count int64) types.Level {
	return types.Level{
		Price:    dec(price),
		Quantity: dec(quantity),
		Count:    count,
	}
}

func dec(value string) decimal.Decimal {
	parsed, err := decimal.NewFromString(value)
	if err != nil {
		panic(err)
	}
	return parsed
}

func assertLevels(t *testing.T, got, want []types.Level) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("level count = %d, want %d; got %#v", len(got), len(want), got)
	}
	for i := range want {
		if !got[i].Price.Equal(want[i].Price) ||
			!got[i].Quantity.Equal(want[i].Quantity) ||
			got[i].Count != want[i].Count {
			t.Fatalf("level[%d] = %#v, want %#v", i, got[i], want[i])
		}
	}
}

func snapshotKey(snapshot *types.DepthUpdate) string {
	return fmt.Sprintf("bids=%s asks=%s", levelsKey(snapshot.Bids), levelsKey(snapshot.Asks))
}

func levelsKey(levels []types.Level) string {
	key := ""
	for _, level := range levels {
		key += fmt.Sprintf("%s/%s/%d;", level.Price.String(), level.Quantity.String(), level.Count)
	}
	return key
}
