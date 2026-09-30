package ws

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/tent-of-trials/market/orderbook"
	"github.com/tent-of-trials/market/types"
)

func feedBook(t *testing.T) *orderbook.OrderBook {
	t.Helper()
	book := orderbook.NewOrderBook("BTC-USD", orderbook.Config{MaxDepth: 5})
	err := book.ApplySnapshot(types.DepthUpdate{Symbol: "BTC-USD", Bids: []types.Level{{Price: decimal.NewFromInt(100), Quantity: decimal.NewFromInt(1), Count: 1}}, Asks: []types.Level{{Price: decimal.NewFromInt(101), Quantity: decimal.NewFromInt(1), Count: 1}}}, 10)
	if err != nil {
		t.Fatal(err)
	}
	return book
}

const validFeedDelta = `{"symbol":"BTC-USD","previous_sequence":10,"sequence":11,"updates":[{"side":"bid","price":"100","quantity":"2"}]}`

func TestInvalidFeedPayloadPreservesSnapshotAndSequence(t *testing.T) {
	tests := map[string]string{
		"malformed JSON":      "{",
		"wrong symbol":        strings.Replace(validFeedDelta, "BTC-USD", "ETH-USD", 1),
		"missing symbol":      strings.Replace(validFeedDelta, `"symbol":"BTC-USD",`, "", 1),
		"invalid side":        strings.Replace(validFeedDelta, `"bid"`, `"middle"`, 1),
		"missing quantity":    strings.Replace(validFeedDelta, `,"quantity":"2"`, "", 1),
		"null quantity":       strings.Replace(validFeedDelta, `"quantity":"2"`, `"quantity":null`, 1),
		"missing price":       strings.Replace(validFeedDelta, `"price":"100",`, "", 1),
		"null price":          strings.Replace(validFeedDelta, `"price":"100"`, `"price":null`, 1),
		"nonnumeric price":    strings.Replace(validFeedDelta, `"100"`, `"NaN"`, 1),
		"bad quantity type":   strings.Replace(validFeedDelta, `"quantity":"2"`, `"quantity":{}`, 1),
		"stale sequence":      strings.Replace(validFeedDelta, `"sequence":11`, `"sequence":10`, 1),
		"wrong predecessor":   strings.Replace(validFeedDelta, `"previous_sequence":10`, `"previous_sequence":9`, 1),
		"zero predecessor":    strings.Replace(validFeedDelta, `"previous_sequence":10`, `"previous_sequence":0`, 1),
		"missing predecessor": strings.Replace(validFeedDelta, `"previous_sequence":10,`, "", 1),
		"missing updates":     `{"symbol":"BTC-USD","previous_sequence":10,"sequence":11}`,
		"checksum mismatch":   strings.Replace(validFeedDelta, `"updates"`, `"checksum":"wrong","updates"`, 1),
	}
	for _, side := range []string{"bid", "ask"} {
		payload := strings.Replace(validFeedDelta, `"bid"`, `"`+side+`"`, 1)
		tests[side+" zero price"] = strings.Replace(payload, `"100"`, `"0"`, 1)
		tests[side+" negative price"] = strings.Replace(payload, `"100"`, `"-1"`, 1)
		tests[side+" negative quantity"] = strings.Replace(payload, `"quantity":"2"`, `"quantity":"-1"`, 1)
	}
	for name, payload := range tests {
		t.Run(name, func(t *testing.T) {
			book := feedBook(t)
			before := book.GetSnapshot()
			checksum := orderbook.DepthChecksum(before.Bids, before.Asks)
			if err := ApplyDepthDelta(book, []byte(payload)); err == nil {
				t.Fatal("invalid payload accepted")
			}
			after := book.GetSnapshot()
			if checksum != orderbook.DepthChecksum(after.Bids, after.Asks) {
				t.Fatal("invalid payload mutated book")
			}
			if err := ApplyDepthDelta(book, []byte(validFeedDelta)); err != nil {
				t.Fatalf("valid next sequence rejected after invalid payload: %v", err)
			}
		})
	}
}

func TestSnapshotThenValidFeedDeltas(t *testing.T) {
	book := feedBook(t)
	if err := ApplyDepthDelta(book, []byte(validFeedDelta)); err != nil {
		t.Fatal(err)
	}
	bids := []types.Level{{Price: decimal.NewFromInt(99), Quantity: decimal.NewFromInt(3), Count: 1}}
	asks := book.GetSnapshot().Asks
	delta := orderbook.DepthDelta{Symbol: "BTC-USD", PreviousSequence: 11, Sequence: 12, Updates: []orderbook.DepthDeltaLevel{
		{Side: "bid", Price: decimal.NewFromInt(100), Quantity: decimal.Zero},
		{Side: "bid", Price: decimal.NewFromInt(99), Quantity: decimal.NewFromInt(3)},
	}, Checksum: orderbook.DepthChecksum(bids, asks)}
	payload, err := json.Marshal(delta)
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyDepthDelta(book, payload); err != nil {
		t.Fatal(err)
	}
	got := book.GetSnapshot()
	if orderbook.DepthChecksum(got.Bids, got.Asks) != delta.Checksum {
		t.Fatal("unexpected recovered book")
	}
}
