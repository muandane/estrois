package handlers

import (
	"encoding/json"
	"net/http"
	"sync/atomic"
	"time"
)

type CacheStats struct {
	Hits             uint64    `json:"hits"`
	Misses           uint64    `json:"misses"`
	CurrentSize      int64     `json:"current_size_bytes"`
	MaxSize          int64     `json:"max_size_bytes"`
	EntryCount       int       `json:"entry_count"`
	LastCleanupTime  time.Time `json:"last_cleanup_time"`
	TotalRequests    uint64    `json:"total_requests"`
	CacheHitRatio    float64   `json:"cache_hit_ratio"`
	AvgResponseTime  float64   `json:"avg_response_time_ms"`
	CompressionRatio float64   `json:"compression_ratio"`
}

type StatsHandler struct {
	stats *CacheStats
}

func NewStatsHandler() *StatsHandler {
	return &StatsHandler{
		stats: &CacheStats{
			MaxSize: 100 * 1024 * 1024, // 100MB
		},
	}
}

func (h *StatsHandler) RecordHit() {
	atomic.AddUint64(&h.stats.Hits, 1)
	atomic.AddUint64(&h.stats.TotalRequests, 1)
}

func (h *StatsHandler) RecordMiss() {
	atomic.AddUint64(&h.stats.Misses, 1)
	atomic.AddUint64(&h.stats.TotalRequests, 1)
}

func (h *StatsHandler) UpdateSize(size int64) {
	atomic.StoreInt64(&h.stats.CurrentSize, size)
}

func (h *StatsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	hits := atomic.LoadUint64(&h.stats.Hits)
	total := atomic.LoadUint64(&h.stats.TotalRequests)

	if total > 0 {
		h.stats.CacheHitRatio = float64(hits) / float64(total) * 100
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(h.stats)
}
