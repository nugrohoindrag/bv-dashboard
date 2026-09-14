// Package metrics: RED metrics per route, pool DB, kedalaman antrian job, sweep lag (TAD §11.5).
// Diekspos di /metrics pada alamat internal terpisah (BV_METRICS_ADDR) untuk di-scrape Prometheus.
package metrics

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	HTTPRequests  = prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: "bv", Subsystem: "http", Name: "requests_total", Help: "Jumlah request HTTP per route/method/status."}, []string{"route", "method", "status"})
	HTTPDuration  = prometheus.NewHistogramVec(prometheus.HistogramOpts{Namespace: "bv", Subsystem: "http", Name: "request_duration_seconds", Help: "Latensi request HTTP.", Buckets: []float64{.01, .025, .05, .1, .25, .5, 1, 2, 5}}, []string{"route", "method"})
	JobsProcessed = prometheus.NewCounterVec(prometheus.CounterOpts{Namespace: "bv", Subsystem: "jobs", Name: "processed_total", Help: "Job River selesai per kind/status."}, []string{"kind", "status"})
	JobDuration   = prometheus.NewHistogramVec(prometheus.HistogramOpts{Namespace: "bv", Subsystem: "jobs", Name: "duration_seconds", Help: "Durasi job River.", Buckets: prometheus.ExponentialBuckets(0.01, 3, 8)}, []string{"kind"})
	QueueDepth    = prometheus.NewGaugeVec(prometheus.GaugeOpts{Namespace: "bv", Subsystem: "jobs", Name: "queue_depth", Help: "Job River menunggu (available) per queue."}, []string{"queue"})
	SweepLag      = prometheus.NewGaugeVec(prometheus.GaugeOpts{Namespace: "bv", Subsystem: "sweep", Name: "lag_seconds", Help: "Detik sejak sweep terakhir sukses per jenis (overdue/sla/generator)."}, []string{"sweep"})
	DBPoolTotal   = prometheus.NewGaugeVec(prometheus.GaugeOpts{Namespace: "bv", Subsystem: "db", Name: "pool_conns", Help: "Koneksi pool pgx per state."}, []string{"state"})
)

var registry = prometheus.NewRegistry()

func init() {
	registry.MustRegister(HTTPRequests, HTTPDuration, JobsProcessed, JobDuration, QueueDepth, SweepLag, DBPoolTotal)
	registry.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
}

// Handler mengembalikan http.Handler /metrics.
func Handler() http.Handler { return promhttp.HandlerFor(registry, promhttp.HandlerOpts{}) }

// Middleware mencatat RED per route pattern chi (bukan path mentah agar kardinalitas rendah).
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &recorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(rec, r)
		route := chi.RouteContext(r.Context()).RoutePattern()
		if route == "" {
			route = "unmatched"
		}
		HTTPRequests.WithLabelValues(route, r.Method, strconv.Itoa(rec.status)).Inc()
		HTTPDuration.WithLabelValues(route, r.Method).Observe(time.Since(start).Seconds())
	})
}

type recorder struct {
	http.ResponseWriter
	status int
}

func (r *recorder) WriteHeader(c int) { r.status = c; r.ResponseWriter.WriteHeader(c) }
func (r *recorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Serve menjalankan server /metrics + /health internal di addr (kosong = nonaktif).
func Serve(ctx context.Context, addr string, log *slog.Logger) {
	if addr == "" {
		return
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", Handler())
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		log.Info("metrics listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Warn("metrics server", "err", err)
		}
	}()
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
}

// CollectPool memperbarui gauge pool pgx setiap interval.
func CollectPool(ctx context.Context, pool *pgxpool.Pool, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		st := pool.Stat()
		DBPoolTotal.WithLabelValues("total").Set(float64(st.TotalConns()))
		DBPoolTotal.WithLabelValues("idle").Set(float64(st.IdleConns()))
		DBPoolTotal.WithLabelValues("acquired").Set(float64(st.AcquiredConns()))
		DBPoolTotal.WithLabelValues("max").Set(float64(st.MaxConns()))
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// CollectQueueDepth membaca kedalaman antrian River (state available/retryable) per queue.
func CollectQueueDepth(ctx context.Context, pool *pgxpool.Pool, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		rows, err := pool.Query(ctx, `SELECT queue, count(*) FROM river_job WHERE state IN ('available','retryable','scheduled') GROUP BY queue`)
		if err == nil {
			seen := map[string]bool{}
			for rows.Next() {
				var q string
				var n int64
				if rows.Scan(&q, &n) == nil {
					QueueDepth.WithLabelValues(q).Set(float64(n))
					seen[q] = true
				}
			}
			rows.Close()
			if !seen["default"] {
				QueueDepth.WithLabelValues("default").Set(0)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// MarkSweep mencatat waktu sweep sukses; lag dihitung oleh CollectSweepLag.
var lastSweep sync.Map // name → time.Time

func MarkSweep(name string) { lastSweep.Store(name, time.Now()) }

// CollectSweepLag memperbarui gauge lag tiap interval (alert TAD: > 5 menit).
func CollectSweepLag(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		lastSweep.Range(func(k, v any) bool {
			SweepLag.WithLabelValues(k.(string)).Set(time.Since(v.(time.Time)).Seconds())
			return true
		})
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
