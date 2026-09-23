package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/Omar0Gamal/cartwright/services/orchestrator/internal/api"
	"github.com/Omar0Gamal/cartwright/services/orchestrator/internal/billingclient"
	"github.com/Omar0Gamal/cartwright/services/orchestrator/internal/store"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	tp, err := initTracer(context.Background())
	if err == nil {
		defer func() { _ = tp.Shutdown(context.Background()) }()
	}

	port := envOr("HTTP_ADDR", ":8080")
	billingAddr := envOr("BILLING_ADDR", "localhost:50051")
	natsURL := envOr("NATS_URL", "nats://localhost:4222")
	dbURL := envOr("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/orchestrator?sslmode=disable")

	slog.Info("running migrations")
	m, err := migrate.New("file://migrations", dbURL)
	if err != nil {
		slog.Error("failed to init migrations", "err", err)
		os.Exit(1)
	}
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		slog.Error("failed to apply migrations", "err", err)
		os.Exit(1)
	}

	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		slog.Error("failed to connect to database", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	bclient, err := billingclient.New(billingAddr)
	if err != nil {
		slog.Error("failed to create billing client", "err", err)
		os.Exit(1)
	}

	nc, err := nats.Connect(natsURL)
	if err != nil {
		slog.Error("failed to connect to NATS", "err", err)
		os.Exit(1)
	}
	defer nc.Close()

	js, err := jetstream.New(nc)
	if err != nil {
		slog.Error("failed to create JetStream context", "err", err)
		os.Exit(1)
	}

	_, err = js.CreateOrUpdateStream(context.Background(), jetstream.StreamConfig{
		Name:     "ORDERS",
		Subjects: []string{"orders.>"},
	})
	if err != nil {
		slog.Warn("failed to create/update stream", "err", err)
	}

	pgStore := store.NewPostgresStore(pool)
	handler := api.NewHandler(pgStore, bclient, js)

	// Background work uses a cancellable context so shutdown is clean.
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	workerID := fmt.Sprintf("worker-%d", os.Getpid())
	go handler.StartWorker(ctx, workerID)
	go api.StartOutboxPublisher(ctx, pgStore, js)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/orders", handler.HandleCreateOrder)
	mux.HandleFunc("GET /v1/orders/{id}", handler.HandleGetOrder)

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := pgStore.Ping(); err != nil {
			slog.Warn("readyz: database unreachable", "err", err)
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("database unreachable"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("READY"))
	})

	otelMux := otelhttp.NewHandler(mux, "orchestrator-http")

	srv := &http.Server{Addr: port, Handler: otelMux}

	go func() {
		<-ctx.Done()
		slog.Info("shutting down HTTP server")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	slog.Info("orchestrator listening", "addr", port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("server error", "err", err)
		os.Exit(1)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
