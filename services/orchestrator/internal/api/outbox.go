package api

import (
	"context"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/Omar0Gamal/cartwright/services/orchestrator/internal/store"
)

func StartOutboxPublisher(ctx context.Context, st store.Store, js jetstream.JetStream) {
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = st.PruneOutboxEvents(ctx, 24*time.Hour)
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			slog.Info("outbox publisher stopping")
			return
		default:
		}

		events, err := st.ClaimOutboxEvents(10)
		if err != nil {
			slog.Error("outbox: failed to claim events", "err", err)
			time.Sleep(2 * time.Second)
			continue
		}

		for _, ev := range events {
			topic := "orders.confirmed"
			if ev.Type == "OrderCancelled" {
				topic = "orders.cancelled"
			}

			msg := &nats.Msg{
				Subject: topic,
				Data:    ev.Payload,
				Header:  nats.Header{"Nats-Msg-Id": []string{ev.ID}},
			}
			_, pubErr := js.PublishMsg(ctx, msg)
			if pubErr != nil {
				slog.Error("outbox: publish failed", "event_id", ev.ID, "err", pubErr)
			} else {
				_ = st.MarkOutboxEventPublished(ev.ID)
				slog.Info("outbox: published", "event_id", ev.ID, "subject", topic)
			}
		}

		if len(events) == 0 {
			time.Sleep(1 * time.Second)
		}
	}
}
