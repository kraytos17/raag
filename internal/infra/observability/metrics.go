package observability

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// MetricsConfig controls the metrics HTTP endpoint.
type MetricsConfig struct {
	Enabled bool
	Port    int
	Path    string
}

// Metrics exposes the raag Prometheus metrics.
type Metrics struct {
	Registry *prometheus.Registry

	StreamsStarted    *prometheus.CounterVec
	StreamErrors      *prometheus.CounterVec
	TrackPlays        prometheus.Counter
	ConnectedPeers    prometheus.Gauge
	LibraryTrackCount prometheus.Gauge
	RingBufferFill    *prometheus.GaugeVec
	StreamOpenSeconds *prometheus.HistogramVec
	SearchLatency     *prometheus.HistogramVec
	IPCDuration       *prometheus.HistogramVec
	BufferUnderruns   prometheus.Counter

	server *http.Server
}

// NewMetrics constructs the metric registry and collectors.
func NewMetrics() *Metrics {
	reg := prometheus.NewRegistry()
	m := &Metrics{
		Registry: reg,
		StreamsStarted: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "raag_streams_started_total",
			Help: "Streams started, labeled by source.",
		}, []string{"source"}),
		StreamErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "raag_stream_errors_total",
			Help: "Stream errors by reason.",
		}, []string{"reason"}),
		TrackPlays: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "raag_track_plays_total",
			Help: "Total track play events.",
		}),
		ConnectedPeers: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "raag_connected_peers",
			Help: "Currently connected P2P peers.",
		}),
		LibraryTrackCount: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "raag_library_track_count",
			Help: "Total tracks in library.",
		}),
		RingBufferFill: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "raag_ring_buffer_fill_ratio",
			Help: "Buffer fill per active stream.",
		}, []string{"peer_id"}),
		StreamOpenSeconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "raag_stream_open_duration_seconds",
			Help:    "Time to open stream per peer.",
			Buckets: prometheus.DefBuckets,
		}, []string{"peer_id"}),
		SearchLatency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "raag_search_latency_seconds",
			Help:    "Search query latency.",
			Buckets: prometheus.DefBuckets,
		}, []string{}),
		IPCDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "raag_ipc_request_duration_seconds",
			Help:    "IPC handler latency by command.",
			Buckets: prometheus.DefBuckets,
		}, []string{"command"}),
		BufferUnderruns: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "raag_buffer_underruns_total",
			Help: "Playback buffer underruns.",
		}),
	}
	reg.MustRegister(
		m.StreamsStarted, m.StreamErrors, m.TrackPlays, m.ConnectedPeers,
		m.LibraryTrackCount, m.RingBufferFill, m.StreamOpenSeconds,
		m.SearchLatency, m.IPCDuration, m.BufferUnderruns,
	)
	return m
}

// Start runs the metrics HTTP server on addr (host:port) at path. It is
// non-blocking; the server shuts down when ctx is canceled.
func (m *Metrics) Start(ctx context.Context, addr, path string) error {
	if path == "" {
		path = "/metrics"
	}
	mux := http.NewServeMux()
	mux.Handle(path, promhttp.HandlerFor(m.Registry, promhttp.HandlerOpts{}))

	m.server = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("metrics: listen on %s: %w", addr, err)
	}
	// Update Addr so callers (and tests) can discover the bound port when
	// addr uses port 0.
	m.server.Addr = ln.Addr().String()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = m.server.Shutdown(shutdownCtx)
	}()

	go func() {
		if err := m.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			slog.Error("metrics server error", "error", err)
		}
	}()
	slog.Info("metrics server started", "addr", addr, "path", path)
	return nil
}
