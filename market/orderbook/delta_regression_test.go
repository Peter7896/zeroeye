package orderbook

import (
	"errors"
	"github.com/shopspring/decimal"
	"github.com/tent-of-trials/market/types"
	"testing"
)

func TestDeltaDepthLimitAppliedAfterWholeBatch(t *testing.T) {
	for _, side := range []string{"bid", "ask"} {
		t.Run(side, func(t *testing.T) {
			book := NewOrderBook("BTC-USD", Config{MaxDepth: 1})
			initial, replacement := "100", "99"
			snapshot := types.DepthUpdate{Symbol: "BTC-USD", Bids: []types.Level{level(initial, "1", 1)}}
			if side == "ask" {
				replacement = "101"
				snapshot.Bids = nil
				snapshot.Asks = []types.Level{level(initial, "1", 1)}
			}
			if err := book.ApplySnapshot(snapshot, 10); err != nil {
				t.Fatal(err)
			}
			if err := book.ApplyDelta(validDelta(11, 10,
				DepthDeltaLevel{Side: side, Price: dec(replacement), Quantity: dec("2")},
				DepthDeltaLevel{Side: side, Price: dec(initial), Quantity: decimal.Zero},
			)); err != nil {
				t.Fatal(err)
			}
			got := book.GetSnapshot().Bids
			if side == "ask" {
				got = book.GetSnapshot().Asks
			}
			assertLevels(t, got, []types.Level{level(replacement, "2", 1)})
		})
	}
}

func TestMissingPreviousSequenceDoesNotBypassContinuity(t *testing.T) {
	book := seededBook(t)
	before, updated := snapshotKey(book.GetSnapshot()), book.updatedAt
	err := book.ApplyDelta(validDelta(12, 0, DepthDeltaLevel{Side: "bid", Price: dec("100"), Quantity: dec("2")}))
	if !errors.Is(err, ErrInvalidDeltaSequence) {
		t.Fatalf("got %v, want sequence error", err)
	}
	if snapshotKey(book.GetSnapshot()) != before || book.sequence != 10 || !book.updatedAt.Equal(updated) {
		t.Fatal("invalid delta changed state")
	}
}

func TestLaterInvalidUpdateRollsBackWholeBatch(t *testing.T) {
	book := seededBook(t)
	before, updated := snapshotKey(book.GetSnapshot()), book.updatedAt
	err := book.ApplyDelta(validDelta(11, 10,
		DepthDeltaLevel{Side: "bid", Price: dec("100"), Quantity: dec("2")},
		DepthDeltaLevel{Side: "ask", Price: dec("102"), Quantity: dec("-1")}))
	if !errors.Is(err, ErrInvalidDeltaQuantity) {
		t.Fatalf("got %v", err)
	}
	if snapshotKey(book.GetSnapshot()) != before || book.sequence != 10 || !book.updatedAt.Equal(updated) {
		t.Fatal("invalid batch changed state")
	}
}

func TestInvalidSnapshotPreservesExistingBook(t *testing.T) {
	book := seededBook(t)
	before, updated := snapshotKey(book.GetSnapshot()), book.updatedAt
	err := book.ApplySnapshot(types.DepthUpdate{Symbol: "BTC-USD", Bids: []types.Level{level("99", "2", 1)}, Asks: []types.Level{level("0", "1", 1)}}, 20)
	if !errors.Is(err, ErrInvalidDeltaPrice) {
		t.Fatalf("got %v", err)
	}
	if snapshotKey(book.GetSnapshot()) != before || book.sequence != 10 || !book.updatedAt.Equal(updated) {
		t.Fatal("invalid snapshot changed existing book")
	}
}

func TestSnapshotDepthLimitAppliedAfterNormalization(t *testing.T) {
	book := NewOrderBook("BTC-USD", Config{MaxDepth: 1})
	err := book.ApplySnapshot(types.DepthUpdate{Symbol: "BTC-USD", Bids: []types.Level{level("100", "1", 1), level("99", "2", 1), level("100", "0", 0)}}, 10)
	if err != nil {
		t.Fatal(err)
	}
	assertLevels(t, book.GetSnapshot().Bids, []types.Level{level("99", "2", 1)})
}
