package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	MessageCounter = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "telegram_messages_total",
			Help: "Total number of messages received per chat",
		},
		[]string{"chat_id"},
	)

	// Mem0 ingestion metrics.

	Mem0CapturedTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "mem0_messages_captured_total",
			Help: "Total number of eligible Telegram messages enqueued for Mem0.",
		},
	)

	Mem0BatchSendTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "mem0_batch_send_total",
			Help: "Mem0 batch send count by projection and outcome.",
		},
		[]string{"projection", "outcome"},
	)

	Mem0RequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "mem0_request_duration_seconds",
			Help:    "Mem0 request latency by operation and outcome.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"operation", "outcome"},
	)

	Mem0CommandTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "mem0_command_total",
			Help: "/memory command invocations by subcommand and outcome.",
		},
		[]string{"subcommand", "outcome"},
	)
)
