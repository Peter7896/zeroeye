package orderbook

import (
	"strings"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/tent-of-trials/market/types"
)

const testSymbol = types.Symbol("BTC-USD")

func TestApplySnapshotThenValidDeltas(t *testing.T) {
	book := newTestBook()
	snapshot := SnapshotUpdate{
		Symbol:   testSymbol,
		Sequence: 10,
		Bids: []types.Level{
			level("100.00", "1.5"),
			level("99.50", "2.0"),
		},
		Asks: []types.Level{
			level("101.00", "1.25"),
			level("102.00", "3.0"),
		},
	}
	snapshot.Checksum = ComputeBookChecksum(snapshot.Symbol, snapshot.Sequence, snapshot.Bids, snapshot.Asks)

	if err := book.ApplySnapshot(snapshot); err != nil {
		t.Fatalf("ApplySnapshot returned error: %v", err)
	}
	assertBookState(t, book, 10, []types.Level{
		level("100.00", "1.5"),
		level("99.50", "2.0"),
	}, []types.Level{
		level("101.00", "1.25"),
		level("102.00", "3.0"),
	})

	delta := DeltaUpdate{
		Symbol:           testSymbol,
		PreviousSequence: 10,
		Sequence:         11,
		Levels: []LevelUpdate{
			{Side: SideBid, Price: dec("100.00"), Quantity: dec("1.75")},
			{Side: SideAsk, Price: dec("101.00"), Quantity: decimal.Zero},
			{Side: SideAsk, Price: dec("100.75"), Quantity: dec("0.5")},
		},
	}
	expectedBids := []types.Level{
		level("100.00", "1.75"),
		level("99.50", "2.0"),
	}
	expectedAsks := []types.Level{
		level("100.75", "0.5"),
		level("102.00", "3.0"),
	}
	delta.Checksum = ComputeBookChecksum(delta.Symbol, delta.Sequence, expectedBids, expectedAsks)

	if err := book.ApplyDelta(delta); err != nil {
		t.Fatalf("ApplyDelta returned error: %v", err)
	}
	assertBookState(t, book, 11, expectedBids, expectedAsks)
}

func TestApplyDeltaRejectsMalformedFieldsWithoutMutation(t *testing.T) {
	tests := []struct {
		name    string
		delta   DeltaUpdate
		wantErr string
	}{
		{
			name: "invalid price",
			delta: baseDelta(LevelUpdate{
				Side:     SideBid,
				Price:    decimal.Zero,
				Quantity: dec("1"),
			}),
			wantErr: "levels[0].price",
		},
		{
			name: "invalid quantity",
			delta: baseDelta(LevelUpdate{
				Side:     SideBid,
				Price:    dec("100"),
				Quantity: dec("-1"),
			}),
			wantErr: "levels[0].quantity",
		},
		{
			name: "invalid side",
			delta: baseDelta(LevelUpdate{
				Side:     UpdateSide("middle"),
				Price:    dec("100"),
				Quantity: dec("1"),
			}),
			wantErr: "levels[0].side",
		},
		{
			name: "wrong symbol",
			delta: DeltaUpdate{
				Symbol:           types.Symbol("ETH-USD"),
				PreviousSequence: 10,
				Sequence:         11,
				Levels: []LevelUpdate{{
					Side:     SideBid,
					Price:    dec("100"),
					Quantity: dec("1"),
				}},
			},
			wantErr: "symbol",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			book := seededBook(t)
			err := book.ApplyDelta(tt.delta)
			if err == nil {
				t.Fatalf("ApplyDelta returned nil error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tt.wantErr)
			}
			assertSeededBookUnchanged(t, book)
		})
	}
}

func TestApplyDeltaRejectsStaleAndOutOfOrderWithoutMutation(t *testing.T) {
	tests := []struct {
		name    string
		delta   DeltaUpdate
		wantErr string
	}{
		{
			name: "stale sequence",
			delta: DeltaUpdate{
				Symbol:           testSymbol,
				PreviousSequence: 9,
				Sequence:         10,
				Levels: []LevelUpdate{{
					Side:     SideBid,
					Price:    dec("100"),
					Quantity: dec("2"),
				}},
			},
			wantErr: "sequence",
		},
		{
			name: "out of order previous sequence",
			delta: DeltaUpdate{
				Symbol:           testSymbol,
				PreviousSequence: 8,
				Sequence:         11,
				Levels: []LevelUpdate{{
					Side:     SideBid,
					Price:    dec("100"),
					Quantity: dec("2"),
				}},
			},
			wantErr: "previous_sequence",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			book := seededBook(t)
			err := book.ApplyDelta(tt.delta)
			if err == nil {
				t.Fatalf("ApplyDelta returned nil error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tt.wantErr)
			}
			assertSeededBookUnchanged(t, book)
		})
	}
}

func TestApplyDeltaRejectsChecksumMismatchWithoutMutation(t *testing.T) {
	book := seededBook(t)
	delta := baseDelta(LevelUpdate{
		Side:     SideBid,
		Price:    dec("100.00"),
		Quantity: dec("2.00"),
	})
	delta.Checksum = "bad-checksum"

	err := book.ApplyDelta(delta)
	if err == nil {
		t.Fatalf("ApplyDelta returned nil error")
	}
	if !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("error %q does not contain checksum", err.Error())
	}
	assertSeededBookUnchanged(t, book)
}

func seededBook(t *testing.T) *OrderBook {
	t.Helper()
	book := newTestBook()
	snapshot := SnapshotUpdate{
		Symbol:   testSymbol,
		Sequence: 10,
		Bids:     []types.Level{level("100.00", "1.00")},
		Asks:     []types.Level{level("101.00", "1.00")},
	}
	snapshot.Checksum = ComputeBookChecksum(snapshot.Symbol, snapshot.Sequence, snapshot.Bids, snapshot.Asks)
	if err := book.ApplySnapshot(snapshot); err != nil {
		t.Fatalf("ApplySnapshot returned error: %v", err)
	}
	return book
}

func assertSeededBookUnchanged(t *testing.T, book *OrderBook) {
	t.Helper()
	assertBookState(t, book, 10, []types.Level{level("100.00", "1.00")}, []types.Level{level("101.00", "1.00")})
}

func baseDelta(level LevelUpdate) DeltaUpdate {
	return DeltaUpdate{
		Symbol:           testSymbol,
		PreviousSequence: 10,
		Sequence:         11,
		Levels:           []LevelUpdate{level},
	}
}

func newTestBook() *OrderBook {
	return NewOrderBook(testSymbol, Config{MaxDepth: 10, PriceDecimals: 8, VolumeDecimals: 8})
}

func assertBookState(t *testing.T, book *OrderBook, wantSequence uint64, wantBids, wantAsks []types.Level) {
	t.Helper()
	if got := book.Sequence(); got != wantSequence {
		t.Fatalf("sequence = %d, want %d", got, wantSequence)
	}
	assertLevels(t, "bids", pointerLevelsToValues(book.GetBids()), wantBids)
	assertLevels(t, "asks", pointerLevelsToValues(book.GetAsks()), wantAsks)
}

func assertLevels(t *testing.T, name string, got, want []types.Level) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s length = %d, want %d: got %#v", name, len(got), len(want), got)
	}
	for i := range want {
		if !got[i].Price.Equal(want[i].Price) || !got[i].Quantity.Equal(want[i].Quantity) || got[i].Count != want[i].Count {
			t.Fatalf("%s[%d] = %#v, want %#v", name, i, got[i], want[i])
		}
	}
}

func pointerLevelsToValues(levels []*types.Level) []types.Level {
	result := make([]types.Level, 0, len(levels))
	for _, level := range levels {
		if level != nil {
			result = append(result, *level)
		}
	}
	return result
}

func level(price, quantity string) types.Level {
	return types.Level{Price: dec(price), Quantity: dec(quantity), Count: 1}
}

func dec(value string) decimal.Decimal {
	return decimal.RequireFromString(value)
}
