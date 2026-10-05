package orderbook

import (
	"errors"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/tent-of-trials/market/types"
)

func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func newTestBook() *OrderBook {
	return NewOrderBook("TEST", Config{MaxDepth: 10, PriceDecimals: 8, VolumeDecimals: 8})
}

func baseSnapshot() Snapshot {
	return Snapshot{
		Symbol: "TEST",
		Bids:   []types.Level{{Price: dec("100.00"), Quantity: dec("1")}, {Price: dec("99.00"), Quantity: dec("2")}},
		Asks:   []types.Level{{Price: dec("100.50"), Quantity: dec("1")}, {Price: dec("101.00"), Quantity: dec("3")}},
		// Sequence 10 so deltas start at 11.
		Sequence: 10,
	}
}

// state captures everything a rejected delta must leave untouched.
type state struct {
	seq  uint64
	bids string
	asks string
}

func snapshotState(t *testing.T, ob *OrderBook) state {
	t.Helper()
	return state{
		seq:  ob.Sequence(),
		bids: levelsKey(ob.GetBids()),
		asks: levelsKey(ob.GetAsks()),
	}
}

func levelsKey(levels []*types.Level) string {
	out := ""
	for _, l := range levels {
		out += l.Price.String() + ":" + l.Quantity.String() + ","
	}
	return out
}

// requirePreserved asserts a rejected delta left the book byte-identical.
func requirePreserved(t *testing.T, ob *OrderBook, before state, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected delta to be rejected, got nil error")
	}
	after := snapshotState(t, ob)
	if after != before {
		t.Fatalf("book mutated by rejected delta:\n before: seq=%d bids=%s asks=%s\n after:  seq=%d bids=%s asks=%s",
			before.seq, before.bids, before.asks,
			after.seq, after.bids, after.asks)
	}
}

// --- Acceptance: cover a valid snapshot followed by valid deltas so the
// recovery path and live update path are tested together. ---

func TestValidSnapshotThenValidDeltas(t *testing.T) {
	ob := newTestBook()
	if err := ob.ApplySnapshot(baseSnapshot()); err != nil {
		t.Fatalf("ApplySnapshot: %v", err)
	}
	if got := ob.Sequence(); got != 10 {
		t.Fatalf("sequence after snapshot = %d, want 10", got)
	}
	if n := len(ob.GetBids()); n != 2 {
		t.Fatalf("bid levels after snapshot = %d, want 2", n)
	}

	// Live update 1: new best bid.
	if err := ob.ApplyDelta(Delta{Symbol: "TEST", Side: types.Buy, Price: dec("100.25"), Quantity: dec("5"), Sequence: 11}); err != nil {
		t.Fatalf("ApplyDelta 11: %v", err)
	}
	// Live update 2: overwrite an existing level.
	if err := ob.ApplyDelta(Delta{Symbol: "TEST", Side: types.Buy, Price: dec("100.25"), Quantity: dec("7"), Sequence: 12}); err != nil {
		t.Fatalf("ApplyDelta 12: %v", err)
	}
	// Live update 3: tombstone an ask (zero quantity removes the level).
	if err := ob.ApplyDelta(Delta{Symbol: "TEST", Side: types.Sell, Price: dec("100.50"), Quantity: dec("0"), Sequence: 13}); err != nil {
		t.Fatalf("ApplyDelta 13: %v", err)
	}

	if got := ob.Sequence(); got != 13 {
		t.Fatalf("sequence = %d, want 13", got)
	}
	bids := ob.GetBids()
	if len(bids) != 3 || !bids[0].Price.Equal(dec("100.25")) || !bids[0].Quantity.Equal(dec("7")) {
		t.Fatalf("bids after deltas = %s", levelsKey(bids))
	}
	asks := ob.GetAsks()
	if len(asks) != 1 || !asks[0].Price.Equal(dec("101.00")) {
		t.Fatalf("asks after tombstone = %s", levelsKey(asks))
	}
}

// --- Acceptance: reject malformed bid/ask price, quantity, side, or symbol
// payloads with a clear error. ---

func TestRejectMalformedDeltaPayloads(t *testing.T) {
	cases := []struct {
		name  string
		delta Delta
		want  error
	}{
		{
			name:  "empty symbol",
			delta: Delta{Side: types.Buy, Price: dec("1"), Quantity: dec("1"), Sequence: 11},
			want:  ErrDeltaSymbol,
		},
		{
			name:  "wrong symbol",
			delta: Delta{Symbol: "OTHER", Side: types.Buy, Price: dec("1"), Quantity: dec("1"), Sequence: 11},
			want:  ErrDeltaSymbol,
		},
		{
			name:  "invalid side",
			delta: Delta{Symbol: "TEST", Side: types.OrderSide(7), Price: dec("1"), Quantity: dec("1"), Sequence: 11},
			want:  ErrDeltaSide,
		},
		{
			name:  "zero price",
			delta: Delta{Symbol: "TEST", Side: types.Buy, Price: dec("0"), Quantity: dec("1"), Sequence: 11},
			want:  ErrDeltaPrice,
		},
		{
			name:  "negative price",
			delta: Delta{Symbol: "TEST", Side: types.Buy, Price: dec("-1"), Quantity: dec("1"), Sequence: 11},
			want:  ErrDeltaPrice,
		},
		{
			name:  "negative quantity",
			delta: Delta{Symbol: "TEST", Side: types.Buy, Price: dec("1"), Quantity: dec("-3"), Sequence: 11},
			want:  ErrDeltaQuantity,
		},
		{
			name:  "zero sequence",
			delta: Delta{Symbol: "TEST", Side: types.Buy, Price: dec("1"), Quantity: dec("1"), Sequence: 0},
			want:  ErrDeltaSequence,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ob := newTestBook()
			if err := ob.ApplySnapshot(baseSnapshot()); err != nil {
				t.Fatalf("ApplySnapshot: %v", err)
			}
			before := snapshotState(t, ob)

			err := ob.ApplyDelta(tc.delta)

			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			// Clear error, not just a sentinel match.
			if err.Error() == tc.want.Error() {
				t.Errorf("error message is bare sentinel %q; want context", err)
			}
			requirePreserved(t, ob, before, err)
		})
	}
}

func TestRejectMalformedSnapshotPayloads(t *testing.T) {
	cases := []struct {
		name string
		snap Snapshot
		want error
	}{
		{
			name: "empty symbol",
			snap: Snapshot{Bids: []types.Level{{Price: dec("1"), Quantity: dec("1")}}},
			want: ErrDeltaSymbol,
		},
		{
			name: "wrong symbol",
			snap: Snapshot{Symbol: "OTHER"},
			want: ErrDeltaSymbol,
		},
		{
			name: "negative bid price",
			snap: Snapshot{Symbol: "TEST", Bids: []types.Level{{Price: dec("-1"), Quantity: dec("1")}}},
			want: ErrDeltaPrice,
		},
		{
			name: "negative ask quantity",
			snap: Snapshot{Symbol: "TEST", Asks: []types.Level{{Price: dec("1"), Quantity: dec("-1")}}},
			want: ErrDeltaQuantity,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ob := newTestBook()
			if err := ob.ApplySnapshot(baseSnapshot()); err != nil {
				t.Fatalf("seed ApplySnapshot: %v", err)
			}
			before := snapshotState(t, ob)

			err := ob.ApplySnapshot(tc.snap)

			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			requirePreserved(t, ob, before, err)
		})
	}
}

// --- Acceptance: reject stale or out-of-order sequence updates without
// mutating the existing order book state. ---

func TestRejectStaleAndOutOfOrderSequences(t *testing.T) {
	cases := []struct {
		name string
		seq  uint64
		want error
	}{
		{name: "replayed current sequence", seq: 10, want: ErrStaleSequence},
		{name: "older sequence", seq: 9, want: ErrStaleSequence},
		{name: "sequence zero already covered", seq: 0, want: ErrDeltaSequence},
		{name: "gap ahead", seq: 12, want: ErrSequenceGap},
		{name: "far ahead", seq: 900, want: ErrSequenceGap},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ob := newTestBook()
			if err := ob.ApplySnapshot(baseSnapshot()); err != nil {
				t.Fatalf("ApplySnapshot: %v", err)
			}
			before := snapshotState(t, ob)

			err := ob.ApplyDelta(Delta{Symbol: "TEST", Side: types.Buy, Price: dec("100.75"), Quantity: dec("9"), Sequence: tc.seq})

			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			requirePreserved(t, ob, before, err)
		})
	}
}

// A stale delta must not advance the sequence, so the *next* correctly
// sequenced delta still applies cleanly.
func TestStaleDeltaDoesNotPoisonSequence(t *testing.T) {
	ob := newTestBook()
	if err := ob.ApplySnapshot(baseSnapshot()); err != nil {
		t.Fatalf("ApplySnapshot: %v", err)
	}

	if err := ob.ApplyDelta(Delta{Symbol: "TEST", Side: types.Buy, Price: dec("100.75"), Quantity: dec("9"), Sequence: 5}); !errors.Is(err, ErrStaleSequence) {
		t.Fatalf("stale delta error = %v, want %v", err, ErrStaleSequence)
	}
	if err := ob.ApplyDelta(Delta{Symbol: "TEST", Side: types.Buy, Price: dec("100.75"), Quantity: dec("9"), Sequence: 11}); err != nil {
		t.Fatalf("valid delta after stale rejection: %v", err)
	}
	if got := ob.Sequence(); got != 11 {
		t.Fatalf("sequence = %d, want 11", got)
	}
}

// A sequence gap must demand a snapshot, and the snapshot must recover the book.
func TestSequenceGapRecoveryViaSnapshot(t *testing.T) {
	ob := newTestBook()
	if err := ob.ApplySnapshot(baseSnapshot()); err != nil {
		t.Fatalf("ApplySnapshot: %v", err)
	}
	before := snapshotState(t, ob)

	err := ob.ApplyDelta(Delta{Symbol: "TEST", Side: types.Buy, Price: dec("100.75"), Quantity: del9(), Sequence: 42})
	if !errors.Is(err, ErrSequenceGap) {
		t.Fatalf("gap delta error = %v, want %v", err, ErrSequenceGap)
	}
	requirePreserved(t, ob, before, err)

	// Recovery: resynchronise with a fresh snapshot at the newer sequence.
	recov := Snapshot{
		Symbol:   "TEST",
		Bids:     []types.Level{{Price: dec("100.75"), Quantity: dec("9")}},
		Asks:     []types.Level{{Price: dec("101.00"), Quantity: dec("3")}},
		Sequence: 42,
	}
	if err := ob.ApplySnapshot(recov); err != nil {
		t.Fatalf("recovery snapshot: %v", err)
	}
	if got := ob.Sequence(); got != 42 {
		t.Fatalf("sequence after recovery = %d, want 42", got)
	}
	// And the feed resumes from the recovered point.
	if err := ob.ApplyDelta(Delta{Symbol: "TEST", Side: types.Sell, Price: dec("101.25"), Quantity: dec("1"), Sequence: 43}); err != nil {
		t.Fatalf("delta after recovery: %v", err)
	}
}

func del9() decimal.Decimal { return dec("9") }

// --- Acceptance: preserve the current book after an invalid delta or
// checksum-style mismatch is encountered. ---

func TestChecksumMismatchPreservesBook(t *testing.T) {
	ob := newTestBook()
	if err := ob.ApplySnapshot(baseSnapshot()); err != nil {
		t.Fatalf("ApplySnapshot: %v", err)
	}
	before := snapshotState(t, ob)

	err := ob.ApplyDelta(Delta{
		Symbol:   "TEST",
		Side:     types.Buy,
		Price:    dec("100.25"),
		Quantity: dec("4"),
		Sequence: 11,
		Checksum: "deadbeef", // not the real post-apply checksum
	})

	if !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("error = %v, want %v", err, ErrChecksumMismatch)
	}
	requirePreserved(t, ob, before, err)
}

// The checksum must be satisfied by a correctly computed value, and the delta
// then applies. This proves the check is meaningful, not always-failing.
func TestCorrectChecksumApplies(t *testing.T) {
	ob := newTestBook()
	if err := ob.ApplySnapshot(baseSnapshot()); err != nil {
		t.Fatalf("ApplySnapshot: %v", err)
	}

	// Build the expected post-apply state, compute its checksum, then send a
	// delta carrying it.
	probe := Delta{Symbol: "TEST", Side: types.Buy, Price: dec("100.25"), Quantity: dec("4"), Sequence: 11}
	if err := ValidateDelta(probe); err != nil {
		t.Fatalf("ValidateDelta: %v", err)
	}
	// Replay the same apply on a throwaway book to learn the real checksum.
	tmp := newTestBook()
	if err := tmp.ApplySnapshot(baseSnapshot()); err != nil {
		t.Fatalf("ApplySnapshot: %v", err)
	}
	if err := tmp.ApplyDelta(probe); err != nil {
		t.Fatalf("probe ApplyDelta: %v", err)
	}
	want := stateChecksum(tmp.GetBids(), tmp.GetAsks())

	probe.Checksum = want
	if err := ob.ApplyDelta(probe); err != nil {
		t.Fatalf("ApplyDelta with correct checksum: %v", err)
	}
	if got := ob.Sequence(); got != 11 {
		t.Fatalf("sequence = %d, want 11", got)
	}
}

// Checksum is order-independent over levels, so equivalent states match.
func TestChecksumIsDeterministicAndOrderIndependent(t *testing.T) {
	a := []*types.Level{
		{Price: dec("100.00"), Quantity: dec("1")},
		{Price: dec("99.00"), Quantity: dec("2")},
	}
	b := []*types.Level{
		{Price: dec("99.00"), Quantity: dec("2")},
		{Price: dec("100.00"), Quantity: dec("1")},
	}
	if stateChecksum(a, nil) != stateChecksum(b, nil) {
		t.Fatal("checksum depends on level ordering")
	}
	if stateChecksum(a, nil) == stateChecksum(b, []*types.Level{{Price: dec("1"), Quantity: dec("1")}}) {
		t.Fatal("checksum ignores the ask side")
	}
}

// --- Closed-book and depth-trim behaviour ---

func TestClosedBookRejectsDeltas(t *testing.T) {
	ob := newTestBook()
	if err := ob.ApplySnapshot(baseSnapshot()); err != nil {
		t.Fatalf("ApplySnapshot: %v", err)
	}
	ob.Close()

	if err := ob.ApplyDelta(Delta{Symbol: "TEST", Side: types.Buy, Price: dec("1"), Quantity: dec("1"), Sequence: 11}); !errors.Is(err, ErrBookClosed) {
		t.Fatalf("ApplyDelta on closed book = %v, want %v", err, ErrBookClosed)
	}
	if err := ob.ApplySnapshot(baseSnapshot()); !errors.Is(err, ErrBookClosed) {
		t.Fatalf("ApplySnapshot on closed book = %v, want %v", err, ErrBookClosed)
	}
}

// A delta adding beyond MaxDepth must trim, but must not discard a level that
// a later delta in the same sequence still needs.
func TestDeltaTrimmingKeepsSubsequentDeltasApplicable(t *testing.T) {
	ob := NewOrderBook("TEST", Config{MaxDepth: 2})
	if err := ob.ApplySnapshot(Snapshot{
		Symbol:   "TEST",
		Bids:     []types.Level{{Price: dec("100"), Quantity: dec("1")}, {Price: dec("99"), Quantity: dec("1")}},
		Sequence: 1,
	}); err != nil {
		t.Fatalf("ApplySnapshot: %v", err)
	}

	// Push a third bid; MaxDepth trims the worst (99).
	if err := ob.ApplyDelta(Delta{Symbol: "TEST", Side: types.Buy, Price: dec("98"), Quantity: dec("1"), Sequence: 2}); err != nil {
		t.Fatalf("ApplyDelta: %v", err)
	}
	if n := len(ob.GetBids()); n != 2 {
		t.Fatalf("bid depth = %d, want 2 (MaxDepth)", n)
	}

	// The trimmed level must still be updatable if the feed re-sends it, and the
	// sequence must keep advancing.
	if err := ob.ApplyDelta(Delta{Symbol: "TEST", Side: types.Buy, Price: dec("97"), Quantity: dec("1"), Sequence: 3}); err != nil {
		t.Fatalf("ApplyDelta after trim: %v", err)
	}
	if got := ob.Sequence(); got != 3 {
		t.Fatalf("sequence = %d, want 3", got)
	}
}

// ValidateDelta must not mutate anything and must be safe to call standalone.
func TestValidateDeltaDoesNotMutate(t *testing.T) {
	ob := newTestBook()
	if err := ob.ApplySnapshot(baseSnapshot()); err != nil {
		t.Fatalf("ApplySnapshot: %v", err)
	}
	before := snapshotState(t, ob)

	if err := ValidateDelta(Delta{Symbol: "TEST", Side: types.Buy, Price: dec("-1"), Quantity: dec("1"), Sequence: 11}); err == nil {
		t.Fatal("expected validation error")
	}
	if err := ValidateDelta(Delta{Symbol: "TEST", Side: types.Sell, Price: dec("1"), Quantity: dec("0"), Sequence: 1}); err != nil {
		t.Fatalf("valid delta rejected: %v", err)
	}

	after := snapshotState(t, ob)
	if after != before {
		t.Fatal("ValidateDelta mutated the book")
	}
}
