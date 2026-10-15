package ws

import (
	"fmt"
	"strings"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/tent-of-trials/market/orderbook"
	"github.com/tent-of-trials/market/types"
)

func TestParseOrderBookDeltaMessageValidBidAskPayload(t *testing.T) {
	payload := []byte(`{
		"type":"depth_delta",
		"symbol":"BTC-USD",
		"previous_sequence":10,
		"sequence":11,
		"bids":[["100.00","1.25"]],
		"asks":[["101.00","0"]],
		"checksum":"abc123"
	}`)

	delta, err := ParseOrderBookDeltaMessage(payload)
	if err != nil {
		t.Fatalf("ParseOrderBookDeltaMessage returned error: %v", err)
	}
	if delta.Symbol != types.Symbol("BTC-USD") {
		t.Fatalf("symbol = %q", delta.Symbol)
	}
	if delta.PreviousSequence != 10 || delta.Sequence != 11 {
		t.Fatalf("sequence = (%d, %d)", delta.PreviousSequence, delta.Sequence)
	}
	if delta.Checksum != "abc123" {
		t.Fatalf("checksum = %q", delta.Checksum)
	}
	if len(delta.Levels) != 2 {
		t.Fatalf("levels length = %d, want 2", len(delta.Levels))
	}
	if delta.Levels[0].Side != orderbook.SideBid || delta.Levels[1].Side != orderbook.SideAsk {
		t.Fatalf("unexpected sides: %#v", delta.Levels)
	}
}

func TestParseOrderBookDeltaMessageValidChangePayload(t *testing.T) {
	payload := []byte(`{
		"type":"orderbook_delta",
		"symbol":"ETH-USD",
		"previous_sequence":7,
		"sequence":8,
		"changes":[
			{"side":"buy","price":"200.50","quantity":"3.0"},
			{"side":"sell","price":"201.00","quantity":"0"}
		]
	}`)

	delta, err := ParseOrderBookDeltaMessage(payload)
	if err != nil {
		t.Fatalf("ParseOrderBookDeltaMessage returned error: %v", err)
	}
	if len(delta.Levels) != 2 {
		t.Fatalf("levels length = %d, want 2", len(delta.Levels))
	}
	if delta.Levels[0].Side != orderbook.SideBid || delta.Levels[1].Side != orderbook.SideAsk {
		t.Fatalf("unexpected sides: %#v", delta.Levels)
	}
}

func TestParseOrderBookDeltaMessageRejectsMalformedPayloads(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		wantErr string
	}{
		{
			name: "invalid bid price",
			payload: `{
				"type":"depth_delta",
				"symbol":"BTC-USD",
				"previous_sequence":10,
				"sequence":11,
				"bids":[["bad-price","1"]]
			}`,
			wantErr: "bids[0].price",
		},
		{
			name: "invalid ask quantity",
			payload: `{
				"type":"depth_delta",
				"symbol":"BTC-USD",
				"previous_sequence":10,
				"sequence":11,
				"asks":[["101","-1"]]
			}`,
			wantErr: "asks[0].quantity",
		},
		{
			name: "invalid side",
			payload: `{
				"type":"depth_delta",
				"symbol":"BTC-USD",
				"previous_sequence":10,
				"sequence":11,
				"changes":[{"side":"middle","price":"100","quantity":"1"}]
			}`,
			wantErr: "changes[0].side",
		},
		{
			name: "missing symbol",
			payload: `{
				"type":"depth_delta",
				"previous_sequence":10,
				"sequence":11,
				"bids":[["100","1"]]
			}`,
			wantErr: "symbol",
		},
		{
			name: "invalid symbol payload type",
			payload: `{
				"type":"depth_delta",
				"symbol":123,
				"previous_sequence":10,
				"sequence":11,
				"bids":[["100","1"]]
			}`,
			wantErr: "symbol",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseOrderBookDeltaMessage([]byte(tt.payload))
			if err == nil {
				t.Fatalf("ParseOrderBookDeltaMessage returned nil error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestParsedOrderBookDeltaAppliesThenRejectsInvalidWithoutMutation(t *testing.T) {
	book := orderbook.NewOrderBook(types.Symbol("BTC-USD"), orderbook.Config{
		MaxDepth:       10,
		PriceDecimals:  2,
		VolumeDecimals: 8,
	})
	initialBids := []types.Level{wsLevel("100.00", "1.00")}
	initialAsks := []types.Level{wsLevel("101.00", "1.00")}
	snapshot := orderbook.SnapshotUpdate{
		Symbol:   types.Symbol("BTC-USD"),
		Sequence: 10,
		Bids:     initialBids,
		Asks:     initialAsks,
		Checksum: orderbook.ComputeBookChecksum(types.Symbol("BTC-USD"), 10, initialBids, initialAsks),
	}
	if err := book.ApplySnapshot(snapshot); err != nil {
		t.Fatalf("ApplySnapshot returned error: %v", err)
	}

	expectedBids := []types.Level{wsLevel("100.00", "2.50")}
	expectedAsks := []types.Level{wsLevel("102.00", "1.00")}
	checksum := orderbook.ComputeBookChecksum(types.Symbol("BTC-USD"), 11, expectedBids, expectedAsks)
	payload := fmt.Sprintf(`{
		"type":"orderbook_delta",
		"symbol":"BTC-USD",
		"previous_sequence":10,
		"sequence":11,
		"changes":[
			{"side":"buy","price":"100.00","quantity":"2.50"},
			{"side":"sell","price":"101.00","quantity":"0"},
			{"side":"sell","price":"102.00","quantity":"1.00"}
		],
		"checksum":%q
	}`, checksum)

	delta, err := ParseOrderBookDeltaMessage([]byte(payload))
	if err != nil {
		t.Fatalf("ParseOrderBookDeltaMessage returned error: %v", err)
	}
	if err := book.ApplyDelta(delta); err != nil {
		t.Fatalf("ApplyDelta returned error: %v", err)
	}
	assertWSBookState(t, book, 11, expectedBids, expectedAsks)

	beforeBids := snapshotLevels(book.GetBids())
	beforeAsks := snapshotLevels(book.GetAsks())
	stale, err := ParseOrderBookDeltaMessage([]byte(`{
		"type":"orderbook_delta",
		"symbol":"BTC-USD",
		"previous_sequence":10,
		"sequence":11,
		"changes":[{"side":"buy","price":"99.00","quantity":"9.00"}]
	}`))
	if err != nil {
		t.Fatalf("ParseOrderBookDeltaMessage stale payload returned error: %v", err)
	}
	err = book.ApplyDelta(stale)
	if err == nil || !strings.Contains(err.Error(), "sequence") {
		t.Fatalf("ApplyDelta stale error = %v, want sequence error", err)
	}
	assertWSBookState(t, book, 11, beforeBids, beforeAsks)

	badChecksum, err := ParseOrderBookDeltaMessage([]byte(`{
		"type":"orderbook_delta",
		"symbol":"BTC-USD",
		"previous_sequence":11,
		"sequence":12,
		"changes":[{"side":"buy","price":"100.00","quantity":"3.00"}],
		"checksum":"wrong"
	}`))
	if err != nil {
		t.Fatalf("ParseOrderBookDeltaMessage checksum payload returned error: %v", err)
	}
	err = book.ApplyDelta(badChecksum)
	if err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("ApplyDelta checksum error = %v, want checksum error", err)
	}
	assertWSBookState(t, book, 11, beforeBids, beforeAsks)
}

func TestParseOrderBookDeltaMessageRejectsUnknownFields(t *testing.T) {
	_, err := ParseOrderBookDeltaMessage([]byte(`{
		"type":"orderbook_delta",
		"symbol":"BTC-USD",
		"previous_sequence":10,
		"sequence":11,
		"changes":[{"side":"buy","price":"100.00","quantity":"2.50"}],
		"unexpected":"field"
	}`))
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("error = %v, want unknown field error", err)
	}
}

func assertWSBookState(t *testing.T, book *orderbook.OrderBook, wantSequence uint64, wantBids, wantAsks []types.Level) {
	t.Helper()
	if got := book.Sequence(); got != wantSequence {
		t.Fatalf("sequence = %d, want %d", got, wantSequence)
	}
	assertWSLevels(t, "bids", snapshotLevels(book.GetBids()), wantBids)
	assertWSLevels(t, "asks", snapshotLevels(book.GetAsks()), wantAsks)
}

func assertWSLevels(t *testing.T, name string, got, want []types.Level) {
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

func snapshotLevels(levels []*types.Level) []types.Level {
	result := make([]types.Level, 0, len(levels))
	for _, level := range levels {
		if level != nil {
			result = append(result, *level)
		}
	}
	return result
}

func wsLevel(price, quantity string) types.Level {
	return types.Level{
		Price:    decimal.RequireFromString(price),
		Quantity: decimal.RequireFromString(quantity),
		Count:    1,
	}
}
