package store

import (
	"context"
	"sync"
	"time"

	"github.com/Omar0Gamal/cartwright/services/orchestrator/internal/saga"
)

type MemoryStore struct {
	mu          sync.RWMutex
	orders      map[string]*Order
	idempotency map[string]string
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		orders:      make(map[string]*Order),
		idempotency: make(map[string]string),
	}
}

func (m *MemoryStore) Ping() error { return nil }

func (m *MemoryStore) CreateOrder(order *Order) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.idempotency[order.IdempotencyKey]; exists {
		return ErrDuplicateKey
	}
	m.orders[order.ID] = order
	m.idempotency[order.IdempotencyKey] = order.ID
	return nil
}

func (m *MemoryStore) GetOrder(id string) (*Order, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	o, ok := m.orders[id]
	return o, ok
}

func (m *MemoryStore) GetOrderByKey(key string) (*Order, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	id, ok := m.idempotency[key]
	if !ok {
		return nil, false
	}
	return m.orders[id], true
}

func (m *MemoryStore) UpdateOrderState(id string, newState saga.OrderState) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if o, ok := m.orders[id]; ok {
		o.Status = newState
	}
}

func (m *MemoryStore) AppendStep(id string, step saga.StepName, status string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if o, ok := m.orders[id]; ok {
		o.Steps = append(o.Steps, StepLog{Step: step, Direction: "forward", Status: status, Attempts: 1})
	}
}

func (m *MemoryStore) ReserveStock(orderID string, items []OrderItem) error  { return nil }
func (m *MemoryStore) ReleaseStock(orderID string) error                     { return nil }
func (m *MemoryStore) ClaimOrders(limit int, ownerID string, d time.Duration) ([]*Order, error) {
	return nil, nil
}
func (m *MemoryStore) RenewLease(id, ownerID string, d time.Duration) error { return nil }
func (m *MemoryStore) RecordStepStarted(orderID string, step saga.StepName, direction string) (string, error) {
	return orderID + "-" + string(step) + "-" + direction, nil
}
func (m *MemoryStore) RecordStepResult(orderID string, step saga.StepName, direction, status string, newState saga.OrderState, eventType string, payload []byte, cancellationReason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if o, ok := m.orders[orderID]; ok {
		o.Status = newState
	}
	return nil
}
func (m *MemoryStore) ClaimOutboxEvents(limit int) ([]*OutboxEvent, error) { return nil, nil }
func (m *MemoryStore) MarkOutboxEventPublished(id string) error            { return nil }

func (m *MemoryStore) PruneOutboxEvents(ctx context.Context, olderThan time.Duration) error { return nil }
