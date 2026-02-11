package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics holds all Prometheus metrics
type Metrics struct {
	// HTTP metrics
	HTTPRequestsTotal   *prometheus.CounterVec
	HTTPRequestDuration *prometheus.HistogramVec
	HTTPResponseSize    *prometheus.HistogramVec

	// Job metrics
	JobsTotal           *prometheus.CounterVec
	JobDuration         *prometheus.HistogramVec
	JobQueueDepth       prometheus.Gauge
	ActiveJobs          prometheus.Gauge
	JobsProcessedTotal  *prometheus.CounterVec

	// FFmpeg operation metrics
	FFmpegOperationsTotal  *prometheus.CounterVec
	FFmpegOperationErrors  *prometheus.CounterVec
	FFmpegProcessingTime   *prometheus.HistogramVec

	// WebSocket metrics
	WebSocketConnections    prometheus.Gauge
	WebSocketMessagesTotal  *prometheus.CounterVec

	// File storage metrics
	StorageFilesTotal      *prometheus.GaugeVec
	StorageBytesTotal      *prometheus.GaugeVec

	// User/Subscription metrics
	ActiveUsers            *prometheus.GaugeVec
	SubscriptionsByTier    *prometheus.GaugeVec
	ConversionMinutesUsed  *prometheus.CounterVec
}

// New creates and registers all metrics
func New() *Metrics {
	m := &Metrics{
		// HTTP metrics
		HTTPRequestsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "http_requests_total",
				Help: "Total number of HTTP requests",
			},
			[]string{"method", "path", "status"},
		),
		HTTPRequestDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "http_request_duration_seconds",
				Help:    "HTTP request latencies in seconds",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"method", "path", "status"},
		),
		HTTPResponseSize: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "http_response_size_bytes",
				Help:    "HTTP response size in bytes",
				Buckets: prometheus.ExponentialBuckets(100, 10, 8),
			},
			[]string{"method", "path", "status"},
		),

		// Job metrics
		JobsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "jobs_total",
				Help: "Total number of jobs created",
			},
			[]string{"status", "operation"},
		),
		JobDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "job_duration_seconds",
				Help:    "Job processing duration in seconds",
				Buckets: []float64{1, 5, 10, 30, 60, 120, 300, 600, 1800, 3600},
			},
			[]string{"operation", "status"},
		),
		JobQueueDepth: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "job_queue_depth",
				Help: "Current number of jobs in queue",
			},
		),
		ActiveJobs: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "active_jobs",
				Help: "Number of currently processing jobs",
			},
		),
		JobsProcessedTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "jobs_processed_total",
				Help: "Total number of jobs processed",
			},
			[]string{"status"},
		),

		// FFmpeg metrics
		FFmpegOperationsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "ffmpeg_operations_total",
				Help: "Total number of FFmpeg operations",
			},
			[]string{"operation", "status"},
		),
		FFmpegOperationErrors: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "ffmpeg_operation_errors_total",
				Help: "Total number of FFmpeg operation errors",
			},
			[]string{"operation", "error_type"},
		),
		FFmpegProcessingTime: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "ffmpeg_processing_time_seconds",
				Help:    "FFmpeg processing time in seconds",
				Buckets: []float64{1, 5, 10, 30, 60, 120, 300, 600, 1800, 3600},
			},
			[]string{"operation"},
		),

		// WebSocket metrics
		WebSocketConnections: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "websocket_connections",
				Help: "Number of active WebSocket connections",
			},
		),
		WebSocketMessagesTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "websocket_messages_total",
				Help: "Total number of WebSocket messages",
			},
			[]string{"type"},
		),

		// Storage metrics
		StorageFilesTotal: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "storage_files_total",
				Help: "Total number of files in storage",
			},
			[]string{"zone"},
		),
		StorageBytesTotal: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "storage_bytes_total",
				Help: "Total storage size in bytes",
			},
			[]string{"zone"},
		),

		// User/Subscription metrics
		ActiveUsers: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "active_users",
				Help: "Number of active users",
			},
			[]string{"tier"},
		),
		SubscriptionsByTier: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "subscriptions_by_tier",
				Help: "Number of subscriptions by tier",
			},
			[]string{"tier", "status"},
		),
		ConversionMinutesUsed: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "conversion_minutes_used_total",
				Help: "Total conversion minutes used",
			},
			[]string{"tier"},
		),
	}

	return m
}

// RecordHTTPRequest records HTTP request metrics
func (m *Metrics) RecordHTTPRequest(method, path string, statusCode int, duration time.Duration, responseSize int64) {
	status := statusCodeToString(statusCode)
	
	m.HTTPRequestsTotal.WithLabelValues(method, path, status).Inc()
	m.HTTPRequestDuration.WithLabelValues(method, path, status).Observe(duration.Seconds())
	if responseSize > 0 {
		m.HTTPResponseSize.WithLabelValues(method, path, status).Observe(float64(responseSize))
	}
}

// RecordJobCreated records job creation
func (m *Metrics) RecordJobCreated(operation string) {
	m.JobsTotal.WithLabelValues("created", operation).Inc()
	m.JobQueueDepth.Inc()
}

// RecordJobStarted records job start
func (m *Metrics) RecordJobStarted() {
	m.ActiveJobs.Inc()
	m.JobQueueDepth.Dec()
}

// RecordJobCompleted records job completion
func (m *Metrics) RecordJobCompleted(operation string, status string, duration time.Duration) {
	m.ActiveJobs.Dec()
	m.JobDuration.WithLabelValues(operation, status).Observe(duration.Seconds())
	m.JobsProcessedTotal.WithLabelValues(status).Inc()
	m.JobsTotal.WithLabelValues(status, operation).Inc()
}

// RecordFFmpegOperation records FFmpeg operation
func (m *Metrics) RecordFFmpegOperation(operation string, success bool, duration time.Duration) {
	status := "success"
	if !success {
		status = "failure"
	}
	
	m.FFmpegOperationsTotal.WithLabelValues(operation, status).Inc()
	m.FFmpegProcessingTime.WithLabelValues(operation).Observe(duration.Seconds())
}

// RecordFFmpegError records FFmpeg error
func (m *Metrics) RecordFFmpegError(operation string, errorType string) {
	m.FFmpegOperationErrors.WithLabelValues(operation, errorType).Inc()
}

// RecordWebSocketConnection records WebSocket connection change
func (m *Metrics) RecordWebSocketConnection(connected bool) {
	if connected {
		m.WebSocketConnections.Inc()
	} else {
		m.WebSocketConnections.Dec()
	}
}

// RecordWebSocketMessage records WebSocket message
func (m *Metrics) RecordWebSocketMessage(messageType string) {
	m.WebSocketMessagesTotal.WithLabelValues(messageType).Inc()
}

// UpdateStorageMetrics updates storage metrics
func (m *Metrics) UpdateStorageMetrics(zone string, fileCount int64, bytes int64) {
	m.StorageFilesTotal.WithLabelValues(zone).Set(float64(fileCount))
	m.StorageBytesTotal.WithLabelValues(zone).Set(float64(bytes))
}

// UpdateUserMetrics updates user metrics
func (m *Metrics) UpdateUserMetrics(tier string, count int) {
	m.ActiveUsers.WithLabelValues(tier).Set(float64(count))
}

// UpdateSubscriptionMetrics updates subscription metrics
func (m *Metrics) UpdateSubscriptionMetrics(tier string, status string, count int) {
	m.SubscriptionsByTier.WithLabelValues(tier, status).Set(float64(count))
}

// RecordConversionMinutes records conversion minutes used
func (m *Metrics) RecordConversionMinutes(tier string, minutes int) {
	m.ConversionMinutesUsed.WithLabelValues(tier).Add(float64(minutes))
}

// statusCodeToString converts HTTP status code to category string
func statusCodeToString(code int) string {
	switch {
	case code >= 200 && code < 300:
		return "2xx"
	case code >= 300 && code < 400:
		return "3xx"
	case code >= 400 && code < 500:
		return "4xx"
	case code >= 500:
		return "5xx"
	default:
		return "unknown"
	}
}
