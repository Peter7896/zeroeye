package orderbook

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/shopspring/decimal"
	"github.com/tent-of-trials/market/types"
)

type DepthDeltaLevel struct {
	Side     string          `json:"side"`
	Price    decimal.Decimal `json:"price"`
	Quantity decimal.Decimal `json:"quantity"`
	Count    int64           `json:"order_count,omitempty"`
}

type DepthDelta struct {
	Symbol           types.Symbol      `json:"symbol"`
	PreviousSequence uint64            `json:"previous_sequence"`
	Sequence         uint64            `json:"sequence"`
	Updates          []DepthDeltaLevel `json:"updates"`
	Checksum         string            `json:"checksum,omitempty"`
}

var (
	ErrInvalidDeltaSymbol    = &BookError{"invalid delta symbol"}
	ErrInvalidDeltaSequence  = &BookError{"invalid delta sequence"}
	ErrInvalidDeltaSide      = &BookError{"invalid delta side"}
	ErrInvalidDeltaPrice     = &BookError{"invalid delta price"}
	ErrInvalidDeltaQuantity  = &BookError{"invalid delta quantity"}
	ErrDeltaChecksumMismatch = &BookError{"delta checksum mismatch"}
)

func (ob *OrderBook) ApplySnapshot(snapshot types.DepthUpdate, sequence uint64) error {
	ob.mu.Lock()
	defer ob.mu.Unlock()

	if ob.closed {
		return ErrBookClosed
	}
	if snapshot.Symbol != ob.symbol {
		return fmt.Errorf("%w: got %q want %q", ErrInvalidDeltaSymbol, snapshot.Symbol, ob.symbol)
	}

	bids, err := normalizeLevels(snapshot.Bids, true, ob.config.MaxDepth)
	if err != nil {
		return err
	}
	asks, err := normalizeLevels(snapshot.Asks, false, ob.config.MaxDepth)
	if err != nil {
		return err
	}

	ob.bids = levelsToPointers(bids)
	ob.asks = levelsToPointers(asks)
	ob.sequence = sequence
	ob.updatedAt = time.Now()
	return nil
}

func (ob *OrderBook) ApplyDelta(delta DepthDelta) error {
	ob.mu.Lock()
	defer ob.mu.Unlock()

	if ob.closed {
		return ErrBookClosed
	}
	if delta.Symbol != ob.symbol {
		return fmt.Errorf("%w: got %q want %q", ErrInvalidDeltaSymbol, delta.Symbol, ob.symbol)
	}
	if delta.Sequence <= ob.sequence {
		return fmt.Errorf("%w: got %d after %d", ErrInvalidDeltaSequence, delta.Sequence, ob.sequence)
	}
	if delta.PreviousSequence != 0 && delta.PreviousSequence != ob.sequence {
		return fmt.Errorf("%w: previous %d does not match current %d", ErrInvalidDeltaSequence, delta.PreviousSequence, ob.sequence)
	}

	bids := clonePointerLevels(ob.bids)
	asks := clonePointerLevels(ob.asks)
	for _, update := range delta.Updates {
		side, err := normalizeSide(update.Side)
		if err != nil {
			return err
		}
		if update.Price.LessThanOrEqual(decimal.Zero) {
			return fmt.Errorf("%w: %s price %s", ErrInvalidDeltaPrice, side, update.Price.String())
		}
		if update.Quantity.IsNegative() {
			return fmt.Errorf("%w: %s quantity %s", ErrInvalidDeltaQuantity, side, update.Quantity.String())
		}
		count := update.Count
		if count <= 0 && update.Quantity.GreaterThan(decimal.Zero) {
			count = 1
		}
		level := types.Level{
			Price:    update.Price,
			Quantity: update.Quantity,
			Count:    count,
		}

		if side == "bid" {
			bids = upsertDeltaLevel(bids, level, true, ob.config.MaxDepth)
		} else {
			asks = upsertDeltaLevel(asks, level, false, ob.config.MaxDepth)
		}
	}

	if delta.Checksum != "" {
		actual := DepthChecksum(bids, asks)
		if !strings.EqualFold(delta.Checksum, actual) {
			return fmt.Errorf("%w: got %s want %s", ErrDeltaChecksumMismatch, actual, delta.Checksum)
		}
	}

	ob.bids = levelsToPointers(bids)
	ob.asks = levelsToPointers(asks)
	ob.sequence = delta.Sequence
	ob.updatedAt = time.Now()
	return nil
}

func DepthChecksum(bids, asks []types.Level) string {
	h := sha256.New()
	writeLevels := func(side string, levels []types.Level) {
		for _, level := range levels {
			fmt.Fprintf(h, "%s:%s:%s:%d;", side, level.Price.String(), level.Quantity.String(), level.Count)
		}
	}
	writeLevels("bid", bids)
	writeLevels("ask", asks)
	return hex.EncodeToString(h.Sum(nil))
}

func normalizeSide(side string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(side)) {
	case "bid", "bids", "buy":
		return "bid", nil
	case "ask", "asks", "sell":
		return "ask", nil
	default:
		return "", fmt.Errorf("%w: %q", ErrInvalidDeltaSide, side)
	}
}

func normalizeLevels(levels []types.Level, bids bool, maxDepth int) ([]types.Level, error) {
	result := make([]types.Level, 0, len(levels))
	for _, level := range levels {
		if level.Price.LessThanOrEqual(decimal.Zero) {
			return nil, fmt.Errorf("%w: snapshot price %s", ErrInvalidDeltaPrice, level.Price.String())
		}
		if level.Quantity.IsNegative() {
			return nil, fmt.Errorf("%w: snapshot quantity %s", ErrInvalidDeltaQuantity, level.Quantity.String())
		}
		if level.Count <= 0 && level.Quantity.GreaterThan(decimal.Zero) {
			level.Count = 1
		}
		result = upsertDeltaLevel(result, level, bids, maxDepth)
	}
	return result, nil
}

func upsertDeltaLevel(levels []types.Level, level types.Level, bids bool, maxDepth int) []types.Level {
	result := make([]types.Level, 0, len(levels)+1)
	for _, existing := range levels {
		if existing.Price.Equal(level.Price) {
			continue
		}
		result = append(result, existing)
	}
	if level.Quantity.GreaterThan(decimal.Zero) {
		result = append(result, level)
	}

	sort.Slice(result, func(i, j int) bool {
		if bids {
			return result[i].Price.GreaterThan(result[j].Price)
		}
		return result[i].Price.LessThan(result[j].Price)
	})
	if maxDepth > 0 && len(result) > maxDepth {
		return result[:maxDepth]
	}
	return result
}

func clonePointerLevels(levels []*types.Level) []types.Level {
	result := make([]types.Level, 0, len(levels))
	for _, level := range levels {
		if level != nil {
			result = append(result, *level)
		}
	}
	return result
}

func levelsToPointers(levels []types.Level) []*types.Level {
	result := make([]*types.Level, 0, len(levels))
	for i := range levels {
		level := levels[i]
		result = append(result, &level)
	}
	return result
}
