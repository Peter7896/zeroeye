package ws

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/tent-of-trials/market/orderbook"
	"github.com/tent-of-trials/market/types"
)

func testBook() *orderbook.OrderBook {
	return orderbook.NewOrderBook("BTC-USD", orderbook.Config{MaxDepth: 10})
}

func TestRejectMalformedDeltaPayloads(t *testing.T) {
	applier := NewDeltaApplier(testBook())
	cases := []OrderBookDelta{
		{Symbol: "", Sequence: 1, Side: "buy", Price: "100", Quantity: "1"},
		{Symbol: "BTC-USD", Sequence: 1, Side: "", Price: "100", Quantity: "1"},
		{Symbol: "BTC-USD", Sequence: 1, Side: "buy", Price: "", Quantity: "1"},
		{Symbol: "BTC-USD", Sequence: 1, Side: "buy", Price: "100", Quantity: ""},
		{Symbol: "BTC-USD", Sequence: 0, Side: "buy", Price: "100", Quantity: "1"},
		{Symbol: "BTC-USD", Sequence: 1, Side: "maybe", Price: "100", Quantity: "1"},
		{Symbol: "BTC-USD", Sequence: 1, Side: "buy", Price: "bad", Quantity: "1"},
		{Symbol: "BTC-USD", Sequence: 1, Side: "buy", Price: "100", Quantity: "-1"},
	}
	for i, delta := range cases {
		if err := applier.ApplyDelta(delta); err == nil {
			t.Fatalf("case %d: expected error for malformed delta", i)
		}
	}
}

func TestRejectStaleOrOutOfOrderSequence(t *testing.T) {
	applier := NewDeltaApplier(testBook())
	snap := &types.DepthUpdate{Symbol: "BTC-USD"}
	if err := applier.ApplySnapshot(snap, "abc"); err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	first := OrderBookDelta{Symbol: "BTC-USD", Sequence: 1, Side: "buy", Price: "100", Quantity: "1", Checksum: "abc"}
	if err := applier.ApplyDelta(first); err != nil {
		t.Fatalf("first delta: %v", err)
	}

	bidsBefore := len(applier.book.GetBids())
	stale := OrderBookDelta{Symbol: "BTC-USD", Sequence: 1, Side: "buy", Price: "101", Quantity: "2", Checksum: "abc"}
	if err := applier.ApplyDelta(stale); err == nil {
		t.Fatal("expected stale sequence error")
	}
	if len(applier.book.GetBids()) != bidsBefore {
		t.Fatal("stale delta must not mutate book")
	}
}

func TestPreserveBookAfterChecksumMismatch(t *testing.T) {
	applier := NewDeltaApplier(testBook())
	if err := applier.ApplySnapshot(&types.DepthUpdate{Symbol: "BTC-USD"}, "good"); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if err := applier.ApplyDelta(OrderBookDelta{
		Symbol: "BTC-USD", Sequence: 1, Side: "buy", Price: "100", Quantity: "1", Checksum: "good",
	}); err != nil {
		t.Fatalf("valid delta: %v", err)
	}

	before := applier.book.GetBids()[0].Price
	err := applier.ApplyDelta(OrderBookDelta{
		Symbol: "BTC-USD", Sequence: 2, Side: "buy", Price: "200", Quantity: "5", Checksum: "bad",
	})
	if err == nil {
		t.Fatal("expected checksum mismatch")
	}
	after := applier.book.GetBids()[0].Price
	if !before.Equal(after) {
		t.Fatal("invalid checksum must preserve book state")
	}
}

func TestSnapshotThenValidDeltas(t *testing.T) {
	applier := NewDeltaApplier(testBook())
	if err := applier.ApplySnapshot(&types.DepthUpdate{
		Symbol: "BTC-USD",
		Bids:   []types.Level{{Price: decimal.RequireFromString("99"), Quantity: decimal.RequireFromString("1")}},
	}, "snap1"); err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	deltas := []OrderBookDelta{
		{Symbol: "BTC-USD", Sequence: 1, Side: "buy", Price: "100", Quantity: "1", Checksum: "snap1"},
		{Symbol: "BTC-USD", Sequence: 2, Side: "sell", Price: "101", Quantity: "2", Checksum: "snap1"},
	}
	for _, d := range deltas {
		if err := applier.ApplyDelta(d); err != nil {
			t.Fatalf("delta seq %d: %v", d.Sequence, err)
		}
	}

	if len(applier.book.GetBids()) == 0 || len(applier.book.GetAsks()) == 0 {
		t.Fatal("expected bids and asks after valid deltas")
	}
	if applier.Sequence() != 2 {
		t.Fatalf("expected sequence 2, got %d", applier.Sequence())
	}
}
