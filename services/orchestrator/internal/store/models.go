package store

import (
	"context"
	"time"

	"github.com/Omar0Gamal/cartwright/services/orchestrator/internal/saga"
)

type OrderItem struct {
	SKU            string `json:"sku"`
	Quantity       int    `json:"quantity"`
	UnitPriceCents int64  `json:"unit_price_cents"`
}

type StepLog struct {
	Step      saga.StepName `json:"step"`
	Direction string        `json:"direction"`
	Status    string        `json:"status"`
	Attempts  int           `json:"attempts"`
}

type Order struct {
	ID                 string          `json:"order_id"`
	IdempotencyKey     string          `json:"-"`
	RequestHash        string          `json:"-"`
	CustomerID         string          `json:"customer_id"`
	Currency           string          `json:"currency"`
	PaymentToken       string          `json:"-"`
	Items              []OrderItem     `json:"-"`
	TotalCents         int64           `json:"total_cents"`
	Status             saga.OrderState `json:"status"`
	CancellationReason *string         `json:"cancellation_reason"`
	Steps              []StepLog       `json:"steps"`
	CreatedAt          time.Time       `json:"-"`
}

type OutboxEvent struct {
	ID      string
	Type    string
	Payload []byte
}

type Store interface {
	CreateOrder(order *Order) error
	GetOrder(id string) (*Order, bool)
	GetOrderByKey(key string) (*Order, bool)
	UpdateOrderState(id string, newState saga.OrderState)
	AppendStep(id string, step saga.StepName, status string)

	ReserveStock(orderID string, items []OrderItem) error
	ReleaseStock(orderID string) error

	ClaimOrders(limit int, ownerID string, leaseDuration time.Duration) ([]*Order, error)
	RenewLease(id string, ownerID string, leaseDuration time.Duration) error
	RecordStepStarted(orderID string, step saga.StepName, direction string) (string, error)
	RecordStepResult(orderID string, step saga.StepName, direction string, status string, newState saga.OrderState, eventType string, payload []byte, cancellationReason string) error

	ClaimOutboxEvents(limit int) ([]*OutboxEvent, error)
	MarkOutboxEventPublished(id string) error
	PruneOutboxEvents(ctx context.Context, olderThan time.Duration) error

	Ping() error
}
