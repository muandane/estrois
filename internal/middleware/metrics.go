package middleware

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/VictoriaMetrics/metrics"
)

type MetricsMiddleware struct {
	requestCounter     *metrics.Counter
	responseTimeHist   *metrics.Histogram
	requestSizeHist    *metrics.Histogram
	responseSizeHist   *metrics.Histogram
	statusCodeCounters map[int]*metrics.Counter
	cacheHitCounter    *metrics.Counter
	cacheMissCounter   *metrics.Counter
	bucketOpsCounter   *metrics.Counter
}

func NewMetricsMiddleware() *MetricsMiddleware {
	m := &MetricsMiddleware{
		requestCounter:     metrics.NewCounter("http_requests_total"),
		responseTimeHist:   metrics.NewHistogram("http_response_time_seconds"),
		requestSizeHist:    metrics.NewHistogram("http_request_size_bytes"),
		responseSizeHist:   metrics.NewHistogram("http_response_size_bytes"),
		statusCodeCounters: make(map[int]*metrics.Counter),
		cacheHitCounter:    metrics.NewCounter("cache_hits_total"),
		cacheMissCounter:   metrics.NewCounter("cache_misses_total"),
		bucketOpsCounter:   metrics.NewCounter("bucket_operations_total"),
	}

	// Initialize status code counters for common codes
	for _, code := range []int{200, 201, 204, 400, 401, 403, 404, 500} {
		m.statusCodeCounters[code] = metrics.NewCounter(
			"http_response_status_total{code=\"" + strconv.Itoa(code) + "\"}",
		)
	}

	return m
}

func (m *MetricsMiddleware) WithMetrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Track request size
		if r.ContentLength > 0 {
			m.requestSizeHist.Update(float64(r.ContentLength))
		}

		// Use custom response writer to capture status code and size
		lrw := newLoggingResponseWriter(w)

		// Process request
		m.requestCounter.Inc()
		next.ServeHTTP(lrw, r)

		// Record metrics
		duration := time.Since(start).Seconds()
		m.responseTimeHist.Update(duration)

		if counter, exists := m.statusCodeCounters[lrw.statusCode]; exists {
			counter.Inc()
		}

		m.responseSizeHist.Update(float64(lrw.length))

		// Track bucket operations
		segments := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(segments) >= 2 {
			m.bucketOpsCounter.Inc()
		}
	})
}

func (m *MetricsMiddleware) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	metrics.WritePrometheus(w, true)
}
