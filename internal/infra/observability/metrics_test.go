package observability

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"
)

func TestNewMetrics_Collects(t *testing.T) {
	m := NewMetrics()

	m.TrackPlays.Inc()
	m.ConnectedPeers.Set(3)
	m.LibraryTrackCount.Set(100)
	m.StreamsStarted.WithLabelValues("local").Inc()
	m.StreamErrors.WithLabelValues("timeout").Inc()
	m.SearchLatency.WithLabelValues().Observe(0.01)
	m.IPCDuration.WithLabelValues("status").Observe(0.002)

	var buf []byte
	out, err := m.Registry.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, mf := range out {
		buf = append(buf, []byte(*mf.Name+"\n")...)
	}
	text := string(buf)
	for _, want := range []string{"raag_track_plays_total", "raag_connected_peers", "raag_library_track_count", "raag_streams_started_total"} {
		if !contains(text, want) {
			t.Errorf("metrics output missing %q\n%s", want, text)
		}
	}
}

func TestMetricsServer_Serves(t *testing.T) {
	m := NewMetrics()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := m.Start(ctx, "127.0.0.1:0", "/metrics"); err != nil {
		t.Fatalf("start: %v", err)
	}
	// m.server.Addr is set before Serve; find the bound port via the listener.
	addr := m.server.Addr
	if addr == "" {
		t.Fatal("server address not set")
	}

	deadline := time.Now().Add(3 * time.Second)
	var resp *http.Response
	var err error
	for time.Now().Before(deadline) {
		resp, err = http.Get("http://" + addr + "/metrics")
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("get /metrics: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !contains(string(body), "raag_track_plays_total") {
		t.Fatalf("metrics body missing counter\n%s", body)
	}
}

func TestRunAll_HealthChecks(t *testing.T) {
	ok := ComponentHealthCheck("db", func(ctx context.Context) error { return nil })
	bad := ComponentHealthCheck("ffmpeg", func(ctx context.Context) error {
		return &healthTestErr{msg: "not found"}
	})

	results := RunAll(context.Background(), ok, bad)
	if len(results) != 2 {
		t.Fatalf("results len = %d, want 2", len(results))
	}
	if !results[0].OK {
		t.Errorf("expected db check to pass")
	}
	if results[1].OK {
		t.Errorf("expected ffmpeg check to fail")
	}
	if AllOK(results) {
		t.Errorf("AllOK should be false when a check fails")
	}
	if s := FormatResults(results); !contains(s, "FAIL") || !contains(s, "ok") {
		t.Errorf("unexpected format:\n%s", s)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

type healthTestErr struct{ msg string }

func (e *healthTestErr) Error() string { return e.msg }
