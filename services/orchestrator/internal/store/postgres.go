package store

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Omar0Gamal/cartwright/services/orchestrator/internal/saga"
)

var ErrDuplicateKey = errors.New("duplicate idempotency key")

type PostgresStore struct {
	db *pgxpool.Pool
}

func NewPostgresStore(db *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{db: db}
}

func (p *PostgresStore) Ping() error {
	return p.db.Ping(context.Background())
}

func (p *PostgresStore) CreateOrder(order *Order) error {
	ctx := context.Background()

	tx, err := p.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx, `
		INSERT INTO orders (id, client_idempotency_key, request_hash, customer_id, currency, total_cents, status, payment_token)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		order.ID, order.IdempotencyKey, order.RequestHash,
		order.CustomerID, order.Currency, order.TotalCents, string(order.Status), order.PaymentToken,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrDuplicateKey
		}
		return err
	}

	for _, item := range order.Items {
		_, err = tx.Exec(ctx, `
			INSERT INTO order_items (order_id, sku, quantity, unit_price_cents)
			VALUES ($1, $2, $3, $4)`,
			order.ID, item.SKU, item.Quantity, item.UnitPriceCents,
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

func (p *PostgresStore) GetOrderByKey(key string) (*Order, bool) {
	ctx := context.Background()
	var order Order
	var statusStr string
	err := p.db.QueryRow(ctx, `
		SELECT id, customer_id, currency, total_cents, status, cancellation_reason, request_hash
		FROM orders WHERE client_idempotency_key = $1`, key).Scan(
		&order.ID, &order.CustomerID, &order.Currency, &order.TotalCents, &statusStr, &order.CancellationReason, &order.RequestHash,
	)
	if err != nil {
		return nil, false
	}
	order.Status = saga.OrderState(statusStr)
	order.IdempotencyKey = key
	return &order, true
}

func (p *PostgresStore) GetOrder(id string) (*Order, bool) {
	ctx := context.Background()
	var order Order
	var statusStr string
	err := p.db.QueryRow(ctx, `
		SELECT id, client_idempotency_key, customer_id, currency, total_cents, status, cancellation_reason, payment_token
		FROM orders WHERE id = $1`, id).Scan(
		&order.ID, &order.IdempotencyKey, &order.CustomerID, &order.Currency, &order.TotalCents, &statusStr, &order.CancellationReason, &order.PaymentToken,
	)
	if err != nil {
		return nil, false
	}
	order.Status = saga.OrderState(statusStr)

	rows, err := p.db.Query(ctx, `SELECT step, direction, status, attempts FROM saga_steps WHERE order_id = $1 ORDER BY id ASC`, id)
	if err != nil {
		slog.Error("failed to fetch saga steps", "order_id", id, "err", err)
		return &order, true
	}
	defer rows.Close()
	for rows.Next() {
		var sl StepLog
		var stepStr string
		if err := rows.Scan(&stepStr, &sl.Direction, &sl.Status, &sl.Attempts); err != nil {
			slog.Error("failed to scan step row", "order_id", id, "err", err)
			continue
		}
		sl.Step = saga.StepName(stepStr)
		order.Steps = append(order.Steps, sl)
	}

	itemRows, err := p.db.Query(ctx, `SELECT sku, quantity, unit_price_cents FROM order_items WHERE order_id = $1`, id)
	if err != nil {
		slog.Error("failed to fetch order items", "order_id", id, "err", err)
		return &order, true
	}
	defer itemRows.Close()
	for itemRows.Next() {
		var item OrderItem
		if err := itemRows.Scan(&item.SKU, &item.Quantity, &item.UnitPriceCents); err != nil {
			slog.Error("failed to scan item row", "order_id", id, "err", err)
			continue
		}
		order.Items = append(order.Items, item)
	}

	return &order, true
}

func (p *PostgresStore) UpdateOrderState(id string, newState saga.OrderState) {
	ctx := context.Background()
	_, _ = p.db.Exec(ctx, "UPDATE orders SET status = $1, updated_at = NOW() WHERE id = $2", string(newState), id)
}

func (p *PostgresStore) AppendStep(id string, step saga.StepName, status string) {
	ctx := context.Background()
	_, _ = p.db.Exec(ctx, `
		INSERT INTO saga_steps (order_id, step, direction, status, attempts, idempotency_key)
		VALUES ($1, $2, 'forward', $3, 1, $4)`,
		id, string(step), status, fmt.Sprintf("%s-%s", id, step))
}

func (p *PostgresStore) ClaimOrders(limit int, ownerID string, leaseDuration time.Duration) ([]*Order, error) {
	ctx := context.Background()
	query := `
		UPDATE orders 
		SET lease_owner = $1, lease_expires_at = NOW() + $2::interval, updated_at = NOW()
		WHERE id IN (
			SELECT id FROM orders
			WHERE status NOT IN ('CONFIRMED', 'CANCELLED')
			  AND (lease_expires_at IS NULL OR lease_expires_at < NOW())
			ORDER BY created_at ASC
			FOR UPDATE SKIP LOCKED
			LIMIT $3
		)
		RETURNING id, client_idempotency_key, customer_id, currency, total_cents, status
	`
	rows, err := p.db.Query(ctx, query, ownerID, fmt.Sprintf("%d seconds", int(leaseDuration.Seconds())), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var claimed []*Order
	for rows.Next() {
		var o Order
		var statusStr string
		if err := rows.Scan(&o.ID, &o.IdempotencyKey, &o.CustomerID, &o.Currency, &o.TotalCents, &statusStr); err != nil {
			return nil, err
		}
		o.Status = saga.OrderState(statusStr)
		claimed = append(claimed, &o)
	}
	return claimed, nil
}

func (p *PostgresStore) RenewLease(id string, ownerID string, leaseDuration time.Duration) error {
	ctx := context.Background()
	_, err := p.db.Exec(ctx, `
		UPDATE orders SET lease_expires_at = NOW() + $1::interval, updated_at = NOW()
		WHERE id = $2 AND lease_owner = $3`,
		fmt.Sprintf("%d seconds", int(leaseDuration.Seconds())), id, ownerID)
	return err
}

func (p *PostgresStore) RecordStepStarted(orderID string, step saga.StepName, direction string) (string, error) {
	ctx := context.Background()
	ik := fmt.Sprintf("%s-%s-%s", orderID, step, direction)

	_, err := p.db.Exec(ctx, `
		INSERT INTO saga_steps (order_id, step, direction, status, attempts, idempotency_key)
		VALUES ($1, $2, $3, 'STARTED', 1, $4)
		ON CONFLICT (order_id, step, direction) DO UPDATE 
		SET status = 'STARTED', attempts = saga_steps.attempts + 1, started_at = NOW()`,
		orderID, string(step), direction, ik)
	return ik, err
}

func (p *PostgresStore) RecordStepResult(orderID string, step saga.StepName, direction string, status string, newState saga.OrderState, eventType string, payload []byte, cancellationReason string) error {
	ctx := context.Background()
	tx, err := p.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if string(step) != "" {
		_, err = tx.Exec(ctx, `
			UPDATE saga_steps SET status = $1, finished_at = NOW() 
			WHERE order_id = $2 AND step = $3 AND direction = $4`,
			status, orderID, string(step), direction)
		if err != nil {
			return err
		}
	}

	if cancellationReason != "" {
		_, err = tx.Exec(ctx,
			"UPDATE orders SET status = $1, cancellation_reason = $2, updated_at = NOW() WHERE id = $3",
			string(newState), cancellationReason, orderID)
	} else {
		_, err = tx.Exec(ctx,
			"UPDATE orders SET status = $1, updated_at = NOW() WHERE id = $2",
			string(newState), orderID)
	}
	if err != nil {
		return err
	}

	if payload != nil && eventType != "" {
		_, err = tx.Exec(ctx, `
			INSERT INTO outbox (id, aggregate_id, type, payload) 
			VALUES (gen_random_uuid(), $1, $2, $3)`, orderID, eventType, payload)
		if err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

func (p *PostgresStore) ClaimOutboxEvents(limit int) ([]*OutboxEvent, error) {
	ctx := context.Background()
	rows, err := p.db.Query(ctx, `
		UPDATE outbox SET claimed_at = NOW() 
		WHERE id IN (
			SELECT id FROM outbox 
			WHERE published_at IS NULL AND claimed_at IS NULL 
			ORDER BY created_at ASC 
			FOR UPDATE SKIP LOCKED 
			LIMIT $1
		) RETURNING id, type, payload`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []*OutboxEvent
	for rows.Next() {
		ev := &OutboxEvent{}
		if err := rows.Scan(&ev.ID, &ev.Type, &ev.Payload); err != nil {
			return nil, err
		}
		events = append(events, ev)
	}
	return events, nil
}

func (p *PostgresStore) MarkOutboxEventPublished(id string) error {
	ctx := context.Background()
	_, err := p.db.Exec(ctx, "UPDATE outbox SET published_at = NOW() WHERE id = $1", id)
	return err
}

func (p *PostgresStore) ReserveStock(orderID string, items []OrderItem) error {
	ctx := context.Background()
	tx, err := p.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var count int
	err = tx.QueryRow(ctx, "SELECT count(*) FROM reservations WHERE order_id = $1 AND status = 'RESERVED'", orderID).Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil // idempotent — already reserved
	}

	for _, item := range items {
		res, err := tx.Exec(ctx, `
			UPDATE inventory
			SET available = available - $1, reserved = reserved + $1
			WHERE sku = $2 AND available >= $1`,
			item.Quantity, item.SKU,
		)
		if err != nil {
			return err
		}
		if res.RowsAffected() == 0 {
			return fmt.Errorf("insufficient stock for SKU %s", item.SKU)
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO reservations (order_id, sku, quantity, status)
			VALUES ($1, $2, $3, 'RESERVED')
			ON CONFLICT (order_id, sku) DO UPDATE SET status = 'RESERVED', quantity = EXCLUDED.quantity`,
			orderID, item.SKU, item.Quantity,
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

func (p *PostgresStore) ReleaseStock(orderID string) error {
	ctx := context.Background()
	tx, err := p.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, "SELECT sku, quantity FROM reservations WHERE order_id = $1 AND status = 'RESERVED'", orderID)
	if err != nil {
		return err
	}

	type res struct {
		sku string
		qty int
	}
	var list []res
	for rows.Next() {
		var r res
		if err := rows.Scan(&r.sku, &r.qty); err != nil {
			rows.Close()
			return err
		}
		list = append(list, r)
	}
	rows.Close()

	for _, r := range list {
		_, err = tx.Exec(ctx, `
			UPDATE inventory
			SET available = available + $1, reserved = reserved - $1
			WHERE sku = $2`,
			r.qty, r.sku,
		)
		if err != nil {
			return err
		}
	}

	_, err = tx.Exec(ctx, "UPDATE reservations SET status = 'RELEASED' WHERE order_id = $1 AND status = 'RESERVED'", orderID)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (p *PostgresStore) PruneOutboxEvents(ctx context.Context, olderThan time.Duration) error {
	query := "DELETE FROM outbox WHERE published_at < NOW() - $1::interval"
	_, err := p.db.Exec(ctx, query, fmt.Sprintf("%d seconds", int(olderThan.Seconds())))
	return err
}
