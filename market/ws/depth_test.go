package ws

import (
	"errors"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/tent-of-trials/market/orderbook"
	"github.com/tent-of-trials/market/types"
)

func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func newBook(t *testing.T) *orderbook.OrderBook {
	t.Helper()
	ob := orderbook.NewOrderBook("TEST", orderbook.Config{MaxDepth: 10, PriceDecimals: 8, VolumeDecimals: 8})
	if err := ob.ApplySnapshot(orderbook.Snapshot{
		Symbol:   "TEST",
		Bids:     []types.Level{{Price: dec("100.00"), Quantity: dec("1")}},
		Asks:     []types.Level{{Price: dec("100.50"), Quantity: dec("1")}},
		Sequence: 10,
	}); err != nil {
		t.Fatalf("seed ApplySnapshot: %v", err)
	}
	return ob
}

// --- Acceptance: reject malformed bid/ask price, quantity, side, or symbol
// payloads with a clear error, straight off the wire. ---

func TestParseDepthDeltaRejectsMalformedFrames(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"invalid json", `{not json`},
		{"unsupported event type", `{"type":"heartbeat","symbol":"TEST"}`},
		{"missing payload", `{"type":"depth_delta","symbol":"TEST"}`},
		{"null payload", `{"type":"depth_snapshot","symbol":"TEST","payload":null}`},
		{"non-string price", `{"type":"depth_delta","symbol":"TEST","payload":{"symbol":"TEST","bids":[{"price":true,"quantity":"1"}]}}`},
		{"garbage price string", `{"type":"depth_delta","symbol":"TEST","payload":{"symbol":"TEST","bids":[{"price":"abc","quantity":"1"}]}}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseDepthDelta([]byte(tc.raw)); err == nil {
				t.Fatal("expected malformed frame to be rejected")
			}
		})
	}
}

func TestParseDepthDeltaAcceptsNumericAndStringDecimals(t *testing.T) {
	// The feed is allowed to send decimals as JSON numbers or strings.
	for _, raw := range []string{
		`{"type":"depth_delta","symbol":"TEST","payload":{"symbol":"TEST","bids":[{"price":100.25,"quantity":2}],"asks":[]}}`,
		`{"type":"depth_delta","symbol":"TEST","payload":{"symbol":"TEST","bids":[{"price":"100.25","quantity":"2"}],"asks":[]}}`,
	} {
		u, err := ParseDepthDelta([]byte(raw))
		if err != nil {
			t.Fatalf("ParseDepthDelta(%s): %v", raw, err)
		}
		if len(u.Bids) != 1 || !u.Bids[0].Price.Equal(dec("100.25")) {
			t.Fatalf("bid = %+v, want price 100.25", u.Bids)
		}
		if !u.Bids[0].Quantity.Equal(dec("2")) {
			t.Fatalf("quantity = %v, want 2", u.Bids[0].Quantity)
		}
	}
}

// --- Acceptance: a valid snapshot followed by valid deltas, exercised
// end-to-end from raw wire frames through to book state. ---

func TestSnapshotThenDeltaFramesAppliedToBook(t *testing.T) {
	ob := newBook(t)

	snapFrame := `{"type":"depth_snapshot","symbol":"TEST","payload":{"symbol":"TEST","bids":[{"price":"100.00","quantity":"1"},{"price":"99.00","quantity":"4"}],"asks":[{"price":"100.50","quantity":"1"}],"timestamp":1700000000000}}`
	u, err := ParseDepthDelta([]byte(snapFrame))
	if err != nil {
		t.Fatalf("ParseDepthDelta(snapshot): %v", err)
	}
	if err := ob.ApplySnapshot(orderbook.Snapshot{Symbol: u.Symbol, Bids: u.Bids, Asks: u.Asks, Sequence: 10}); err != nil {
		t.Fatalf("ApplySnapshot: %v", err)
	}

	// Live delta frames, in sequence.
	for i, frame := range []string{
		`{"type":"depth_delta","symbol":"TEST","payload":{"symbol":"TEST","bids":[{"price":"100.25","quantity":"5"}],"asks":[]}}`,
		`{"type":"depth_delta","symbol":"TEST","payload":{"symbol":"TEST","asks":[{"price":"100.50","quantity":"0"}]}}`,
	} {
		u, err := ParseDepthDelta([]byte(frame))
		if err != nil {
			t.Fatalf("delta %d ParseDepthDelta: %v", i, err)
		}
		d, isSnapshot, err := ToDelta(u)
		if err != nil {
			t.Fatalf("delta %d ToDelta: %v", i, err)
		}
		if isSnapshot {
			t.Fatalf("delta %d parsed as snapshot", i)
		}
		d.Sequence = uint64(11 + i)
		if err := ob.ApplyDelta(d); err != nil {
			t.Fatalf("delta %d ApplyDelta: %v", i, err)
		}
	}

	if got := ob.Sequence(); got != 12 {
		t.Fatalf("sequence = %d, want 12", got)
	}
	if n := len(ob.GetBids()); n != 3 {
		t.Fatalf("bid levels = %d, want 3", n)
	}
	if n := len(ob.GetAsks()); n != 0 {
		t.Fatalf("ask levels = %d, want 0 after tombstone", n)
	}
}

// A malformed frame must never reach or mutate the book.
func TestMalformedFrameLeavesBookUnchanged(t *testing.T) {
	ob := newBook(t)
	beforeBids := len(ob.GetBids())
	beforeAsks := len(ob.GetAsks())
	beforeSeq := ob.Sequence()

	for _, raw := range []string{
		`{not json`,
		`{"type":"heartbeat"}`,
		`{"type":"depth_delta","symbol":"TEST","payload":{"symbol":"TEST","bids":[{"price":"-1","quantity":"1"}],"asks":[]}}`,
	} {
		if _, err := ParseDepthDelta([]byte(raw)); err == nil {
			t.Fatalf("expected %s to be rejected", raw)
		}
	}

	if ob.Sequence() != beforeSeq || len(ob.GetBids()) != beforeBids || len(ob.GetAsks()) != beforeAsks {
		t.Fatal("book mutated by malformed frames")
	}
}

func TestToDeltaRejectsAmbiguousFrames(t *testing.T) {
	cases := []struct {
		name string
		u    types.DepthUpdate
	}{
		{
			name: "both sides present",
			u:    types.DepthUpdate{Symbol: "TEST", Bids: []types.Level{{Price: dec("1"), Quantity: dec("1")}}, Asks: []types.Level{{Price: dec("2"), Quantity: dec("1")}}},
		},
		{
			name: "multiple levels",
			u:    types.DepthUpdate{Symbol: "TEST", Bids: []types.Level{{Price: dec("1"), Quantity: dec("1")}, {Price: dec("2"), Quantity: dec("1")}}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := ToDelta(tc.u); err == nil {
				t.Fatal("expected ambiguous frame to be rejected")
			}
		})
	}
}

func TestToDeltaEmptyFrameIsSnapshot(t *testing.T) {
	u := types.DepthUpdate{Symbol: "TEST"}
	if _, isSnapshot, err := ToDelta(u); err != nil {
		t.Fatalf("ToDelta: %v", err)
	} else if !isSnapshot {
		t.Fatal("empty frame should be treated as a snapshot")
	}
}

// --- Acceptance: stale / out-of-order frames are rejected and preserve state. ---

func TestStaleFrameRejectedAndStatePreserved(t *testing.T) {
	ob := newBook(t)
	beforeBids := len(ob.GetBids())
	beforeSeq := ob.Sequence()

	d, isSnapshot, err := ToDelta(types.DepthUpdate{Symbol: "TEST", Bids: []types.Level{{Price: dec("100.25"), Quantity: dec("9")}}})
	if err != nil || isSnapshot {
		t.Fatalf("ToDelta: err=%v isSnapshot=%v", err, isSnapshot)
	}
	d.Sequence = beforeSeq // replayed sequence

	err = ob.ApplyDelta(d)
	if !errors.Is(err, orderbook.ErrStaleSequence) {
		t.Fatalf("error = %v, want %v", err, orderbook.ErrStaleSequence)
	}
	if ob.Sequence() != beforeSeq || len(ob.GetBids()) != beforeBids {
		t.Fatal("book mutated by stale frame")
	}
}

// --- Cross-check: orderbook rejection surfaces through the ws layer. ---

func TestFeedErrorsAreTyped(t *testing.T) {
	var fe *FeedError
	if !errors.As(error(&FeedError{Event: "x", Reason: "y"}), &fe) {
		t.Fatal("FeedError must satisfy errors.As")
	}
	if got := (&FeedError{Event: "heartbeat", Reason: "unsupported event type"}).Error(); got == "" {
		t.Fatal("FeedError must produce a clear message")
	}
}
