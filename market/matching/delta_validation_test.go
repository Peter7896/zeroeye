package matching

import (
	"strings"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/tent-of-trials/market/orderbook"
	"github.com/tent-of-trials/market/types"
)

func TestMatchingOwnedBookRejectsInvalidDeltaWithoutMutation(t *testing.T) {
	engine := newDeltaTestEngine()
	book := engine.books[types.Symbol("BTC-USD")]
	seedMatchingBook(t, book)

	beforeBids := matchingSnapshotLevels(book.GetBids())
	beforeAsks := matchingSnapshotLevels(book.GetAsks())
	beforeSequence := book.Sequence()

	err := book.ApplyDelta(orderbook.DeltaUpdate{
		Symbol:           types.Symbol("BTC-USD"),
		PreviousSequence: beforeSequence,
		Sequence:         beforeSequence + 1,
		Levels: []orderbook.LevelUpdate{{
			Side:     orderbook.SideAsk,
			Price:    matchingDec("101.00"),
			Quantity: matchingDec("-0.25"),
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "quantity") {
		t.Fatalf("ApplyDelta error = %v, want quantity validation error", err)
	}

	assertMatchingBookState(t, book, beforeSequence, beforeBids, beforeAsks)
}

func TestMatchingOwnedBookRejectsOutOfOrderDeltaWithoutMutation(t *testing.T) {
	engine := newDeltaTestEngine()
	book := engine.books[types.Symbol("BTC-USD")]
	seedMatchingBook(t, book)

	beforeBids := matchingSnapshotLevels(book.GetBids())
	beforeAsks := matchingSnapshotLevels(book.GetAsks())
	beforeSequence := book.Sequence()

	err := book.ApplyDelta(orderbook.DeltaUpdate{
		Symbol:           types.Symbol("BTC-USD"),
		PreviousSequence: beforeSequence - 1,
		Sequence:         beforeSequence + 1,
		Levels: []orderbook.LevelUpdate{{
			Side:     orderbook.SideBid,
			Price:    matchingDec("100.00"),
			Quantity: matchingDec("2.00"),
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "previous_sequence") {
		t.Fatalf("ApplyDelta error = %v, want previous_sequence validation error", err)
	}

	assertMatchingBookState(t, book, beforeSequence, beforeBids, beforeAsks)
}

func TestMatchingOwnedBookAcceptsSnapshotThenDeltaPath(t *testing.T) {
	engine := newDeltaTestEngine()
	book := engine.books[types.Symbol("BTC-USD")]

	bids := []types.Level{matchingLevel("100.00", "1.00")}
	asks := []types.Level{matchingLevel("101.00", "1.00")}
	snapshot := orderbook.SnapshotUpdate{
		Symbol:   types.Symbol("BTC-USD"),
		Sequence: 42,
		Bids:     bids,
		Asks:     asks,
	}
	snapshot.Checksum = orderbook.ComputeBookChecksum(snapshot.Symbol, snapshot.Sequence, bids, asks)
	if err := book.ApplySnapshot(snapshot); err != nil {
		t.Fatalf("ApplySnapshot error = %v", err)
	}

	wantBids := []types.Level{matchingLevel("100.00", "1.50")}
	wantAsks := []types.Level{matchingLevel("102.00", "0.75")}
	delta := orderbook.DeltaUpdate{
		Symbol:           types.Symbol("BTC-USD"),
		PreviousSequence: 42,
		Sequence:         43,
		Levels: []orderbook.LevelUpdate{
			{Side: orderbook.SideBid, Price: matchingDec("100.00"), Quantity: matchingDec("1.50")},
			{Side: orderbook.SideAsk, Price: matchingDec("101.00"), Quantity: decimal.Zero},
			{Side: orderbook.SideAsk, Price: matchingDec("102.00"), Quantity: matchingDec("0.75")},
		},
	}
	delta.Checksum = orderbook.ComputeBookChecksum(delta.Symbol, delta.Sequence, wantBids, wantAsks)
	if err := book.ApplyDelta(delta); err != nil {
		t.Fatalf("ApplyDelta error = %v", err)
	}

	assertMatchingBookState(t, book, 43, wantBids, wantAsks)
}

func newDeltaTestEngine() *MatchingEngine {
	config := orderbook.Config{MaxDepth: 10, PriceDecimals: 8, VolumeDecimals: 8}
	return NewMatchingEngine(EngineConfig{EnableShorting: true}, map[types.Symbol]*orderbook.OrderBook{
		types.Symbol("BTC-USD"): orderbook.NewOrderBook(types.Symbol("BTC-USD"), config),
	})
}

func seedMatchingBook(t *testing.T, book *orderbook.OrderBook) {
	t.Helper()
	bids := []types.Level{matchingLevel("100.00", "1.00")}
	asks := []types.Level{matchingLevel("101.00", "1.00")}
	snapshot := orderbook.SnapshotUpdate{
		Symbol:   types.Symbol("BTC-USD"),
		Sequence: 10,
		Bids:     bids,
		Asks:     asks,
	}
	snapshot.Checksum = orderbook.ComputeBookChecksum(snapshot.Symbol, snapshot.Sequence, bids, asks)
	if err := book.ApplySnapshot(snapshot); err != nil {
		t.Fatalf("ApplySnapshot error = %v", err)
	}
}

func assertMatchingBookState(t *testing.T, book *orderbook.OrderBook, wantSequence uint64, wantBids, wantAsks []types.Level) {
	t.Helper()
	if got := book.Sequence(); got != wantSequence {
		t.Fatalf("sequence = %d, want %d", got, wantSequence)
	}
	assertMatchingLevels(t, "bids", matchingSnapshotLevels(book.GetBids()), wantBids)
	assertMatchingLevels(t, "asks", matchingSnapshotLevels(book.GetAsks()), wantAsks)
}

func assertMatchingLevels(t *testing.T, name string, got, want []types.Level) {
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

func matchingSnapshotLevels(levels []*types.Level) []types.Level {
	result := make([]types.Level, 0, len(levels))
	for _, level := range levels {
		if level != nil {
			result = append(result, *level)
		}
	}
	return result
}

func matchingLevel(price, quantity string) types.Level {
	return types.Level{Price: matchingDec(price), Quantity: matchingDec(quantity), Count: 1}
}

func matchingDec(value string) decimal.Decimal {
	return decimal.RequireFromString(value)
}
