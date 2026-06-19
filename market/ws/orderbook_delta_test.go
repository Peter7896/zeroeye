package ws

import (
	"encoding/json"
	"testing"
)

func TestOrderBookDeltaValidation(t *testing.T) {
	tests := []struct {
		name       string
		delta      map[string]interface{}
		wantErr    bool
		wantErrMsg string
	} {
		{
			name: "valid delta",
			delta: map[string]interface{}{
				"price": 10.0,
				"quantity": 100,
				"side": "buy",
				"symbol": "BTCUSDT",
			},
			wantErr: false,
		},
		{
			name: "malformed delta",
			delta: map[string]interface{}{
				"price": "invalid",
			},
			wantErr: true,
			wantErrMsg: "invalid price",
		},
		{
			name: "stale delta",
			delta: map[string]interface{}{
				"sequence": 10,
			},
			wantErr: true,
			wantErrMsg: "stale delta",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deltaJSON, err := json.Marshal(tt.delta)
			if err != nil {
				t.Fatal(err)
			}
			// Call the function to validate the delta
			// For this example, we assume the function is called ValidateOrderBookDelta
			if err := ValidateOrderBookDelta(deltaJSON); (err != nil) != tt.wantErr {
				t.Errorf("ValidateOrderBookDelta() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil && err.Error() != tt.wantErrMsg {
				t.Errorf("ValidateOrderBookDelta() error message = %v, want %v", err, tt.wantErrMsg)
			}
		})
	}
}
