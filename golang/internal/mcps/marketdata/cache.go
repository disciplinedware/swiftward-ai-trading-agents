package marketdata

import (
	"sync"
	"time"

	"ai-trading-agents/internal/marketdata"
)

// Cache provides in-memory caching for market data.
// Closed candles are cached indefinitely (they never change).
type Cache struct {
	mu              sync.RWMutex
	candles         map[string][]marketdata.Candle // key: "MARKET:INTERVAL"
	requestedLimits map[string]int                 // limit used when last populating each key
}

// NewCache creates an empty cache.
func NewCache() *Cache {
	return &Cache{
		candles:         make(map[string][]marketdata.Candle),
		requestedLimits: make(map[string]int),
	}
}

// GetCandles returns cached candles if available.
// Returns the last `limit` candles that close before endTime.
// intervalDuration is used to compute candle close time; pass 0 to skip close-time filtering.
// Returns (nil, false) on cache miss.
func (c *Cache) GetCandles(key string, limit int, endTime time.Time, intervalDuration time.Duration) ([]marketdata.Candle, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	cached, ok := c.candles[key]
	if !ok {
		return nil, false
	}
	if len(cached) == 0 {
		// Key was populated but source returned no candles (source exhausted).
		return []marketdata.Candle{}, true
	}

	// Filter by endTime if set: include candles whose close time <= endTime
	// (i.e. fully closed at or before endTime). Matches simulated source semantics.
	result := cached
	if !endTime.IsZero() {
		var filtered []marketdata.Candle
		for _, candle := range cached {
			closeTime := candle.Timestamp
			if intervalDuration > 0 {
				closeTime = candle.Timestamp.Add(intervalDuration)
			}
			if !closeTime.After(endTime) {
				filtered = append(filtered, candle)
			}
		}
		result = filtered
	}

	if len(result) == 0 {
		return nil, false
	}

	// Return last `limit` candles
	if len(result) > limit {
		result = result[len(result)-limit:]
	}

	return result, true
}

// PutCandles stores candles in the cache. Merges with existing entries.
// requestedLimit is the limit that was passed to the source; tracking it allows
// the cache to distinguish "source exhausted" from "cache primed with smaller limit".
func (c *Cache) PutCandles(key string, candles []marketdata.Candle, requestedLimit int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(candles) == 0 {
		// Do not cache empty responses: an empty result may be transient
		// (e.g. no candles have closed yet for a new market). Let each empty
		// response fall through to a real source fetch next time so that data
		// is picked up once it becomes available.
		// Do NOT advance requestedLimits here: an empty response stores nothing,
		// so advancing the limit would cause the exhausted-source heuristic in
		// GetCandlesWithRequestedLimit to wrongly suppress future fetches when
		// the cache holds underfilled data from a prior call.
		return
	}

	// Record the largest limit we have ever requested for this key when data is
	// actually stored. This lets callers detect whether the source is exhausted
	// (returned fewer candles than requested) without being misled by transient
	// empty responses that store nothing but would otherwise inflate the counter.
	if requestedLimit > c.requestedLimits[key] {
		c.requestedLimits[key] = requestedLimit
	}

	existing := c.candles[key]
	if len(existing) == 0 {
		c.candles[key] = candles
		return
	}

	// Bidirectional merge: prepend candles older than the cache's earliest entry
	// (backfill) and append candles newer than the cache's latest entry.
	// Candles within the existing time range are skipped to avoid duplicates.
	// Backfill matters when a larger limit is fetched after a smaller one: the
	// source returns older history that the merge must incorporate so that
	// requestedLimits accurately reflects what is actually stored.
	firstExisting := existing[0].Timestamp
	lastExisting := existing[len(existing)-1].Timestamp
	var older, newer []marketdata.Candle
	for _, candle := range candles {
		if candle.Timestamp.Before(firstExisting) {
			older = append(older, candle)
		} else if candle.Timestamp.After(lastExisting) {
			newer = append(newer, candle)
		}
		// candles within [firstExisting, lastExisting] are already cached; skip.
	}
	merged := make([]marketdata.Candle, 0, len(older)+len(existing)+len(newer))
	merged = append(merged, older...)
	merged = append(merged, existing...)
	merged = append(merged, newer...)
	c.candles[key] = merged
}

// GetRequestedLimit returns the largest limit ever used when populating this key.
// Returns 0 if the key has never been populated.
func (c *Cache) GetRequestedLimit(key string) int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.requestedLimits[key]
}

// GetCandlesWithRequestedLimit returns cached candles and the max requested limit for the key
// under a single lock acquisition, eliminating the race between a separate GetCandles call
// and a subsequent GetRequestedLimit call.
func (c *Cache) GetCandlesWithRequestedLimit(key string, limit int, endTime time.Time, intervalDuration time.Duration) ([]marketdata.Candle, bool, int) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	priorLimit := c.requestedLimits[key]

	cached, ok := c.candles[key]
	if !ok {
		return nil, false, priorLimit
	}
	if len(cached) == 0 {
		return []marketdata.Candle{}, true, priorLimit
	}

	result := cached
	if !endTime.IsZero() {
		var filtered []marketdata.Candle
		for _, candle := range cached {
			closeTime := candle.Timestamp
			if intervalDuration > 0 {
				closeTime = candle.Timestamp.Add(intervalDuration)
			}
			if !closeTime.After(endTime) {
				filtered = append(filtered, candle)
			}
		}
		result = filtered
	}

	if len(result) == 0 {
		return nil, false, priorLimit
	}

	if len(result) > limit {
		result = result[len(result)-limit:]
	}

	return result, true, priorLimit
}
