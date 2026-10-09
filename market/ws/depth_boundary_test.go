package ws

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/tent-of-trials/market/orderbook"
	"github.com/tent-of-trials/market/types"
)

// Apply the real decoding/conversion/book pipeline. The feed adapter supplies
// sequence and optional checksum separately from the legacy DepthUpdate payload.
func applyWireDelta(ob *orderbook.OrderBook, raw string, sequence uint64, checksum string) error {
	u, err := ParseDepthDelta([]byte(raw))
	if err != nil {
		return err
	}
	d, isSnapshot, err := ToDelta(u)
	if err != nil {
		return err
	}
	if isSnapshot {
		return errors.New("expected a single-level delta")
	}
	d.Sequence, d.Checksum = sequence, checksum
	return ob.ApplyDelta(d)
}

func capturedBook(t *testing.T, ob *orderbook.OrderBook) string {
	t.Helper()
	snapshot := ob.GetSnapshot()
	// GetSnapshot.Timestamp is generated at read time, not stored book state.
	state := struct {
		Sequence uint64
		Symbol   types.Symbol
		Bids     []types.Level
		Asks     []types.Level
	}{ob.Sequence(), snapshot.Symbol, snapshot.Bids, snapshot.Asks}
	encoded, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func TestMissingOrNullWireQuantityPreservesBook(t *testing.T) {
	for _, side := range []struct{ name, price string }{{"bids", "100"}, {"asks", "100.50"}} {
		for _, quantity := range []struct{ name, field string }{{"missing", ""}, {"null", `,"quantity":null`}} {
			t.Run(side.name+"/"+quantity.name, func(t *testing.T) {
				ob := newBook(t)
				before := capturedBook(t, ob)
				raw := fmt.Sprintf(`{"type":"depth_delta","symbol":"TEST","payload":{"symbol":"TEST","%s":[{"price":"%s"%s}]}}`, side.name, side.price, quantity.field)
				err := applyWireDelta(ob, raw, 11, "")
				var feedErr *FeedError
				if !errors.As(err, &feedErr) || !strings.Contains(feedErr.Reason, "quantity") {
					t.Errorf("missing/null quantity must fail at the feed boundary, got %v", err)
				}
				if after := capturedBook(t, ob); after != before {
					t.Errorf("malformed frame mutated book:\nbefore: %s\nafter:  %s", before, after)
				}
			})
		}
	}
}

func TestExplicitWireQuantityUpdatesAndDeletes(t *testing.T) {
	for _, side := range []struct{ name, price string }{{"bids", "100"}, {"asks", "100.50"}} {
		for _, quantity := range []struct{ name, value string }{{"numeric", "2"}, {"string", `"2"`}} {
			t.Run(side.name+"/"+quantity.name, func(t *testing.T) {
				ob := newBook(t)
				frame := func(value string) string {
					return fmt.Sprintf(`{"type":"depth_delta","symbol":"TEST","payload":{"symbol":"TEST","%s":[{"price":"%s","quantity":%s}]}}`, side.name, side.price, value)
				}
				if err := applyWireDelta(ob, frame(quantity.value), 11, ""); err != nil {
					t.Fatalf("update: %v", err)
				}
				levels, opposite := ob.GetBids(), ob.GetAsks()
				if side.name == "asks" {
					levels, opposite = opposite, levels
				}
				if ob.Sequence() != 11 || len(levels) != 1 || !levels[0].Quantity.Equal(dec("2")) || len(opposite) != 1 || !opposite[0].Quantity.Equal(dec("1")) {
					t.Fatalf("unexpected update state: %s", capturedBook(t, ob))
				}
				zero := "0"
				if quantity.name == "string" {
					zero = `"0"`
				}
				if err := applyWireDelta(ob, frame(zero), 12, ""); err != nil {
					t.Fatalf("explicit zero deletion: %v", err)
				}
				levels, opposite = ob.GetBids(), ob.GetAsks()
				if side.name == "asks" {
					levels, opposite = opposite, levels
				}
				if ob.Sequence() != 12 || len(levels) != 0 || len(opposite) != 1 || !opposite[0].Quantity.Equal(dec("1")) {
					t.Fatalf("unexpected deletion state: %s", capturedBook(t, ob))
				}
			})
		}
	}
}

func TestDecodedDeltaMetadataRejectionsPreserveBook(t *testing.T) {
	const raw = `{"type":"depth_delta","symbol":"TEST","payload":{"symbol":"TEST","bids":[{"price":"100","quantity":"2"}]}}`
	for _, tc := range []struct {
		name     string
		sequence uint64
		checksum string
		want     error
	}{
		{"missing sequence", 0, "", orderbook.ErrDeltaSequence},
		{"stale sequence", 10, "", orderbook.ErrStaleSequence},
		{"out of order", 9, "", orderbook.ErrStaleSequence},
		{"sequence gap", 12, "", orderbook.ErrSequenceGap},
		{"checksum mismatch", 11, "not-the-post-update-checksum", orderbook.ErrChecksumMismatch},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ob := newBook(t)
			before := capturedBook(t, ob)
			if err := applyWireDelta(ob, raw, tc.sequence, tc.checksum); !errors.Is(err, tc.want) {
				t.Errorf("error = %v, want %v", err, tc.want)
			}
			if after := capturedBook(t, ob); after != before {
				t.Errorf("rejected frame mutated book:\nbefore: %s\nafter:  %s", before, after)
			}
		})
	}
}
