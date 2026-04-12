package prism

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	c := NewClient(Config{
		BaseURL:          srv.URL,
		APIKey:           "test-key",
		Timeout:          5 * time.Second,
		FailureThreshold: 3,
		Cooldown:         60 * time.Second,
	}, zaptest.NewLogger(t))
	return c, srv
}

func TestGetFearGreed(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/market/fear-greed", r.URL.Path)
		assert.Equal(t, "test-key", r.Header.Get("X-API-Key"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"value": 72, "label": "Greed", "warnings": []string{},
		})
	})

	resp, err := c.GetFearGreed(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 72, resp.Value)
	assert.Equal(t, "Greed", resp.Label)
}

func TestGetTechnicals(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/technicals/BTC", r.URL.Path)
		rsi := 45.5
		_ = json.NewEncoder(w).Encode(map[string]any{
			"symbol": "BTC", "timeframe": "daily", "rsi": rsi,
			"overall_signal": "neutral", "bullish_signals": 1, "bearish_signals": 0,
		})
	})

	resp, err := c.GetTechnicals(context.Background(), "BTC")
	require.NoError(t, err)
	assert.Equal(t, "BTC", resp.Symbol)
	require.NotNil(t, resp.RSI)
	assert.InDelta(t, 45.5, *resp.RSI, 0.01)
}

func TestGetSignalsSummary(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/signals/summary", r.URL.Path)
		assert.Equal(t, "ETH,BTC", r.URL.Query().Get("symbols"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"symbol": "ETH", "overall_signal": "bullish", "net_score": 2},
				{"symbol": "BTC", "overall_signal": "neutral", "net_score": 0},
			},
			"metadata": map[string]any{
				"summary": map[string]any{
					"total": 2, "bullish": 1, "neutral": 1,
				},
			},
		})
	})

	resp, err := c.GetSignalsSummary(context.Background(), []string{"ETH", "BTC"})
	require.NoError(t, err)
	assert.Len(t, resp.Data, 2)
	assert.Equal(t, "ETH", resp.Data[0].Symbol)
	assert.Equal(t, "bullish", resp.Data[0].OverallSignal)
	assert.Equal(t, 2, resp.Summary.Total)
}

func TestCircuitBreaker(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(c *Client)
		verify func(t *testing.T, c *Client, err error)
	}{
		{
			name: "opens after threshold consecutive failures",
			setup: func(c *Client) {
				now := time.Now()
				c.now = func() time.Time { return now }
				for i := 0; i < 3; i++ {
					c.afterCall(fmt.Errorf("fail %d", i))
				}
			},
			verify: func(t *testing.T, c *Client, err error) {
				assert.Equal(t, stateOpen, c.State())
				var ue *UnavailableError
				require.ErrorAs(t, err, &ue)
				assert.Equal(t, "circuit breaker open", ue.Reason)
				assert.Greater(t, ue.RetryAfterSecs, 0)
			},
		},
		{
			name: "stays closed under threshold",
			setup: func(c *Client) {
				c.afterCall(fmt.Errorf("fail 1"))
				c.afterCall(fmt.Errorf("fail 2"))
			},
			verify: func(t *testing.T, c *Client, err error) {
				assert.NoError(t, err)
				assert.Equal(t, stateClosed, c.State())
			},
		},
		{
			name: "success resets consecutive errors",
			setup: func(c *Client) {
				c.afterCall(fmt.Errorf("fail 1"))
				c.afterCall(fmt.Errorf("fail 2"))
				c.afterCall(nil) // success resets
				c.afterCall(fmt.Errorf("fail 3"))
				c.afterCall(fmt.Errorf("fail 4"))
			},
			verify: func(t *testing.T, c *Client, err error) {
				assert.NoError(t, err)
				assert.Equal(t, stateClosed, c.State())
			},
		},
		{
			name: "transitions to half-open after cooldown",
			setup: func(c *Client) {
				now := time.Now()
				c.now = func() time.Time { return now }
				for i := 0; i < 3; i++ {
					c.afterCall(fmt.Errorf("fail"))
				}
				// Advance past cooldown.
				c.now = func() time.Time { return now.Add(61 * time.Second) }
			},
			verify: func(t *testing.T, c *Client, err error) {
				assert.NoError(t, err)
				assert.Equal(t, stateHalfOpen, c.State())
			},
		},
		{
			name: "half-open success closes breaker",
			setup: func(c *Client) {
				now := time.Now()
				c.now = func() time.Time { return now }
				for i := 0; i < 3; i++ {
					c.afterCall(fmt.Errorf("fail"))
				}
				c.now = func() time.Time { return now.Add(61 * time.Second) }
				// Allow probe through.
				_ = c.beforeCall()
				// Simulate success.
				c.afterCall(nil)
			},
			verify: func(t *testing.T, c *Client, err error) {
				assert.NoError(t, err)
				assert.Equal(t, stateClosed, c.State())
			},
		},
		{
			name: "half-open failure reopens breaker",
			setup: func(c *Client) {
				now := time.Now()
				c.now = func() time.Time { return now }
				for i := 0; i < 3; i++ {
					c.afterCall(fmt.Errorf("fail"))
				}
				c.now = func() time.Time { return now.Add(61 * time.Second) }
				_ = c.beforeCall()
				c.afterCall(fmt.Errorf("still failing"))
			},
			verify: func(t *testing.T, c *Client, err error) {
				assert.Equal(t, stateOpen, c.State())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{"value": 50, "label": "Neutral"})
			})
			tt.setup(c)
			err := c.beforeCall()
			tt.verify(t, c, err)
		})
	}
}

func TestCircuitBreakerBlocksHTTPCalls(t *testing.T) {
	var callCount atomic.Int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"server error"}`))
	})

	// Make 3 failing calls to trip the breaker.
	for i := 0; i < 3; i++ {
		_, _ = c.GetFearGreed(context.Background())
	}
	assert.Equal(t, int32(3), callCount.Load())
	assert.Equal(t, stateOpen, c.State())

	// Next call should NOT hit the server.
	_, err := c.GetFearGreed(context.Background())
	assert.Equal(t, int32(3), callCount.Load()) // no new HTTP call
	var ue *UnavailableError
	require.ErrorAs(t, err, &ue)
}

func TestCanonicalToBase(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"ETH-USDC", "ETH"},
		{"BTC-USDC", "BTC"},
		{"SOL", "SOL"},
		{"DOGE-USDT", "DOGE"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, CanonicalToBase(tt.input))
	}
}
