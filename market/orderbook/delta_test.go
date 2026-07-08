package orderbook

import (
	"errors"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/tent-of-trials/market/types"
)

func TestApplyDeltaRejectsMalformedLevelsWithoutMutatingState(t *testing.T) {
	book := seededBook(t)
	before := book.GetSnapshot()

	err := book.ApplyDelta(Delta{
		Symbol:   "BTC-USDC",
		Sequence: book.Sequence() + 1,
		Bids: []types.Level{{
			Price:    decimal.RequireFromString("-1"),
			Quantity: decimal.RequireFromString("0.5"),
			Count:    1,
		}},
	})

	if !errors.Is(err, ErrInvalidDeltaPrice) {
		t.Fatalf("expected invalid price error, got %v", err)
	}
	assertSnapshotEqual(t, before, book.GetSnapshot())
}

func TestApplyDeltaRejectsStaleSequenceWithoutMutatingState(t *testing.T) {
	book := seededBook(t)
	before := book.GetSnapshot()

	err := book.ApplyDelta(Delta{
		Symbol:   "BTC-USDC",
		Sequence: book.Sequence(),
		Asks: []types.Level{{
			Price:    decimal.RequireFromString("103"),
			Quantity: decimal.RequireFromString("1"),
			Count:    1,
		}},
	})

	if !errors.Is(err, ErrInvalidDeltaSequence) {
		t.Fatalf("expected stale sequence error, got %v", err)
	}
	assertSnapshotEqual(t, before, book.GetSnapshot())
}

func TestApplyDeltaRejectsChecksumMismatchWithoutMutatingState(t *testing.T) {
	book := seededBook(t)
	before := book.GetSnapshot()

	err := book.ApplyDelta(Delta{
		Symbol:   "BTC-USDC",
		Sequence: book.Sequence() + 1,
		Asks: []types.Level{{
			Price:    decimal.RequireFromString("103"),
			Quantity: decimal.RequireFromString("1"),
			Count:    1,
		}},
		Checksum: "not-the-next-book-checksum",
	})

	if !errors.Is(err, ErrInvalidDeltaChecksum) {
		t.Fatalf("expected checksum error, got %v", err)
	}
	assertSnapshotEqual(t, before, book.GetSnapshot())
}

func TestReplaceSnapshotThenApplyValidDelta(t *testing.T) {
	book := NewOrderBook("ETH-USDC", Config{MaxDepth: 100})
	err := book.ReplaceSnapshot(&types.DepthUpdate{
		Symbol: "ETH-USDC",
		Bids: []types.Level{{
			Price:    decimal.RequireFromString("100"),
			Quantity: decimal.RequireFromString("2"),
			Count:    1,
		}},
		Asks: []types.Level{{
			Price:    decimal.RequireFromString("101"),
			Quantity: decimal.RequireFromString("3"),
			Count:    1,
		}},
	}, 10)
	if err != nil {
		t.Fatalf("replace snapshot: %v", err)
	}

	nextBids := []*types.Level{
		{
			Price:    decimal.RequireFromString("100"),
			Quantity: decimal.RequireFromString("1.5"),
			Count:    1,
		},
	}
	nextAsks := []*types.Level{
		{
			Price:    decimal.RequireFromString("102"),
			Quantity: decimal.RequireFromString("4"),
			Count:    1,
		},
	}
	checksum := ComputeChecksum(nextBids, nextAsks)

	err = book.ApplyDelta(Delta{
		Symbol:   "ETH-USDC",
		Sequence: 11,
		Bids: []types.Level{{
			Price:    decimal.RequireFromString("100"),
			Quantity: decimal.RequireFromString("1.5"),
			Count:    1,
		}},
		Asks: []types.Level{
			{
				Price:    decimal.RequireFromString("101"),
				Quantity: decimal.Zero,
				Count:    0,
			},
			{
				Price:    decimal.RequireFromString("102"),
				Quantity: decimal.RequireFromString("4"),
				Count:    1,
			},
		},
		Checksum: checksum,
	})
	if err != nil {
		t.Fatalf("apply delta: %v", err)
	}

	snapshot := book.GetSnapshot()
	if got := snapshot.Bids[0].Quantity.String(); got != "1.5" {
		t.Fatalf("bid quantity = %s", got)
	}
	if got := snapshot.Asks[0].Price.String(); got != "102" {
		t.Fatalf("ask price = %s", got)
	}
	if got := book.Sequence(); got != 11 {
		t.Fatalf("sequence = %d", got)
	}
}

func seededBook(t *testing.T) *OrderBook {
	t.Helper()
	book := NewOrderBook("BTC-USDC", Config{MaxDepth: 100})
	err := book.ReplaceSnapshot(&types.DepthUpdate{
		Symbol: "BTC-USDC",
		Bids: []types.Level{{
			Price:    decimal.RequireFromString("100"),
			Quantity: decimal.RequireFromString("2"),
			Count:    1,
		}},
		Asks: []types.Level{{
			Price:    decimal.RequireFromString("101"),
			Quantity: decimal.RequireFromString("3"),
			Count:    1,
		}},
	}, 5)
	if err != nil {
		t.Fatalf("seed book: %v", err)
	}
	return book
}

func assertSnapshotEqual(t *testing.T, want *types.DepthUpdate, got *types.DepthUpdate) {
	t.Helper()
	if len(want.Bids) != len(got.Bids) || len(want.Asks) != len(got.Asks) {
		t.Fatalf("snapshot size changed: want %+v got %+v", want, got)
	}
	for i := range want.Bids {
		if !want.Bids[i].Price.Equal(got.Bids[i].Price) || !want.Bids[i].Quantity.Equal(got.Bids[i].Quantity) {
			t.Fatalf("bid[%d] changed: want %+v got %+v", i, want.Bids[i], got.Bids[i])
		}
	}
	for i := range want.Asks {
		if !want.Asks[i].Price.Equal(got.Asks[i].Price) || !want.Asks[i].Quantity.Equal(got.Asks[i].Quantity) {
			t.Fatalf("ask[%d] changed: want %+v got %+v", i, want.Asks[i], got.Asks[i])
		}
	}
}
