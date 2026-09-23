package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	eventspb "github.com/Omar0Gamal/cartwright/services/orchestrator/gen/events/v1"
	"github.com/Omar0Gamal/cartwright/services/orchestrator/internal/billingclient"
	"github.com/Omar0Gamal/cartwright/services/orchestrator/internal/saga"
	"github.com/Omar0Gamal/cartwright/services/orchestrator/internal/store"
)

type CreateOrderRequest struct {
	CustomerID   string            `json:"customer_id"`
	Currency     string            `json:"currency"`
	PaymentToken string            `json:"payment_token"`
	Items        []store.OrderItem `json:"items"`
}

type CreateOrderResponse struct {
	OrderID    string          `json:"order_id"`
	Status     saga.OrderState `json:"status"`
	TotalCents int64           `json:"total_cents"`
}

type Handler struct {
	store   store.Store
	bclient *billingclient.Client
	js      jetstream.JetStream
}

func NewHandler(s store.Store, bclient *billingclient.Client, js jetstream.JetStream) *Handler {
	return &Handler{store: s, bclient: bclient, js: js}
}

func (h *Handler) HandleCreateOrder(w http.ResponseWriter, r *http.Request) {
	idemKey := r.Header.Get("Idempotency-Key")
	if idemKey == "" {
		http.Error(w, "Idempotency-Key header required", http.StatusBadRequest)
		return
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusInternalServerError)
		return
	}
	hash := sha256.Sum256(bodyBytes)
	reqHash := hex.EncodeToString(hash[:])

	var req CreateOrderRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if len(req.Items) == 0 || req.CustomerID == "" || req.Currency == "" || req.PaymentToken == "" {
		http.Error(w, "missing required fields", http.StatusBadRequest)
		return
	}

	var total int64
	for _, item := range req.Items {
		total += item.UnitPriceCents * int64(item.Quantity)
	}

	orderID := uuid.New().String()
	order := &store.Order{
		ID:             orderID,
		IdempotencyKey: idemKey,
		RequestHash:    reqHash,
		CustomerID:     req.CustomerID,
		Currency:       req.Currency,
		PaymentToken:   req.PaymentToken,
		Items:          req.Items,
		TotalCents:     total,
		Status:         saga.StatePending,
		Steps:          []store.StepLog{},
		CreatedAt:      time.Now(),
	}

	if err := h.store.CreateOrder(order); err != nil {
		if errors.Is(err, store.ErrDuplicateKey) {
			// Lost the race — another request with this key already inserted.
			// Re-fetch and handle like a normal idempotency hit.
			h.returnExistingOrder(w, idemKey, reqHash)
			return
		}
		slog.Error("failed to create order", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	go h.driveSaga(context.Background(), orderID, "inline-worker")

	// If ?wait is set, block until the order reaches a terminal state or the timeout.
	if waitStr := r.URL.Query().Get("wait"); waitStr != "" {
		if secs, parseErr := strconv.Atoi(waitStr); parseErr == nil && secs > 0 {
			order = h.pollUntilTerminal(orderID, time.Duration(secs)*time.Second)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Location", "/v1/orders/"+orderID)
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(CreateOrderResponse{
		OrderID:    orderID,
		Status:     order.Status,
		TotalCents: order.TotalCents,
	})
}

func (h *Handler) returnExistingOrder(w http.ResponseWriter, idemKey, reqHash string) {
	existing, ok := h.store.GetOrderByKey(idemKey)
	if !ok {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if existing.RequestHash != reqHash {
		http.Error(w, "idempotency key reused with different payload", http.StatusUnprocessableEntity)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Location", "/v1/orders/"+existing.ID)
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(CreateOrderResponse{
		OrderID:    existing.ID,
		Status:     existing.Status,
		TotalCents: existing.TotalCents,
	})
}

func (h *Handler) pollUntilTerminal(orderID string, timeout time.Duration) *store.Order {
	deadline := time.After(timeout)
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-deadline:
			o, _ := h.store.GetOrder(orderID)
			return o
		case <-tick.C:
			if o, ok := h.store.GetOrder(orderID); ok && saga.IsTerminal(o.Status) {
				return o
			}
		}
	}
}

func (h *Handler) HandleGetOrder(w http.ResponseWriter, r *http.Request) {
	orderID := r.PathValue("id")
	if orderID == "" {
		http.Error(w, "missing order id", http.StatusNotFound)
		return
	}

	order, ok := h.store.GetOrder(orderID)
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(order)
}

// StartWorker runs the recovery loop that picks up stalled sagas.
func (h *Handler) StartWorker(ctx context.Context, workerID string) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			orders, err := h.store.ClaimOrders(10, workerID, 15*time.Second)
			if err != nil {
				slog.Error("recovery: failed to claim orders", "err", err)
				continue
			}
			for _, o := range orders {
				go h.driveSaga(ctx, o.ID, workerID)
			}
		}
	}
}

func (h *Handler) driveSaga(ctx context.Context, orderID string, workerID string) {
	renewCtx, cancelRenew := context.WithCancel(ctx)
	defer cancelRenew()

	go func() {
		tick := time.NewTicker(4 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-renewCtx.Done():
				return
			case <-tick.C:
				_ = h.store.RenewLease(orderID, workerID, 15*time.Second)
			}
		}
	}()

	log := slog.With("order_id", orderID, "worker", workerID)

	for {
		select {
		case <-ctx.Done():
			log.Info("context cancelled, stopping saga")
			return
		default:
		}

		order, ok := h.store.GetOrder(orderID)
		if !ok || saga.IsTerminal(order.Status) {
			return
		}

		history := make([]saga.StepResult, len(order.Steps))
		for i, sl := range order.Steps {
			history[i] = saga.StepResult{
				Name:      sl.Step,
				Direction: sl.Direction,
				Status:    sl.Status,
			}
		}

		step := saga.NextStep(order.Status, history)
		if step == nil {
			if order.Status == saga.StateCompensating {
				h.finishCompensation(order, log)
			}
			return
		}

		direction := "forward"
		if order.Status == saga.StateCompensating {
			direction = "backward"
		}

		ik, err := h.store.RecordStepStarted(orderID, *step, direction)
		if err != nil {
			log.Error("failed to record step start", "step", *step, "err", err)
			return
		}

		grpcCtx := metadata.AppendToOutgoingContext(ctx, "x-correlation-id", orderID)

		var actionErr error
		if direction == "forward" {
			actionErr = h.executeForwardStep(grpcCtx, order, *step, ik, log)
			if actionErr == errDeclined {
				return // already handled inside executeForwardStep
			}
		} else {
			actionErr = h.executeCompensation(grpcCtx, order, *step, ik, log)
		}

		stepStatus := "SUCCEEDED"
		if actionErr != nil {
			stepStatus = "FAILED"
			log.Warn("step failed", "step", *step, "direction", direction, "err", actionErr)
		}

		newState := saga.Advance(order.Status, *step, stepStatus == "SUCCEEDED", direction)

		var payload []byte
		var eventType string
		if *step == saga.StepConfirmOrder && stepStatus == "SUCCEEDED" {
			event := &eventspb.OrderConfirmed{
				EventId:       uuid.New().String(),
				OrderId:       order.ID,
				CustomerId:    order.CustomerID,
				TotalCents:    order.TotalCents,
				Currency:      order.Currency,
				OccurredAt:    timestamppb.Now(),
				CorrelationId: order.ID,
			}
			payload, err = proto.Marshal(event)
			if err != nil {
				log.Error("failed to marshal OrderConfirmed event", "err", err)
				return
			}
			eventType = "OrderConfirmed"
		}

		err = h.store.RecordStepResult(orderID, *step, direction, stepStatus, newState, eventType, payload, "")
		if err != nil {
			log.Error("failed to record step result", "err", err)
			return
		}

		// On failure, continue the loop — re-read order state (now COMPENSATING)
		// and start compensation immediately instead of waiting for the recovery tick.
	}
}

var errDeclined = errors.New("payment declined")

func (h *Handler) executeForwardStep(ctx context.Context, order *store.Order, step saga.StepName, ik string, log *slog.Logger) error {
	if os.Getenv("CARTWRIGHT_FAULTS") == "1" && order.CustomerID == "cust_orch_fault" && step == saga.StepAuthorizePayment {
		log.Warn("Orchestrator fault hook active — delaying before authorize")
		time.Sleep(10 * time.Second)
	}

	switch step {
	case saga.StepAuthorizePayment:
		result, err := h.bclient.Authorize(ctx, order.ID, ik, order.PaymentToken, order.TotalCents, order.Currency)
		if err != nil {
			return err
		}
		if result.Declined {
			log.Info("payment declined, cancelling order", "reason", result.DeclineReason)
			reason := "Payment declined: " + result.DeclineReason
			event := &eventspb.OrderCancelled{
				EventId:       uuid.New().String(),
				OrderId:       order.ID,
				CustomerId:    order.CustomerID,
				Reason:        reason,
				OccurredAt:    timestamppb.Now(),
				CorrelationId: order.ID,
			}
			payload, marshalErr := proto.Marshal(event)
			if marshalErr != nil {
				log.Error("failed to marshal OrderCancelled", "err", marshalErr)
				return marshalErr
			}
			_ = h.store.RecordStepResult(order.ID, step, "forward", "DECLINED", saga.StateCancelled, "OrderCancelled", payload, reason)
			return errDeclined
		}
		return nil
	case saga.StepReserveStock:
		return h.store.ReserveStock(order.ID, order.Items)
	case saga.StepCapturePayment:
		return h.bclient.Capture(ctx, order.ID, ik)
	case saga.StepConfirmOrder:
		return nil // outbox event is written atomically in RecordStepResult
	}
	return nil
}

func (h *Handler) executeCompensation(ctx context.Context, order *store.Order, step saga.StepName, ik string, log *slog.Logger) error {
	switch step {
	case saga.StepAuthorizePayment:
		return h.bclient.Void(ctx, order.ID, ik)
	case saga.StepReserveStock:
		return h.store.ReleaseStock(order.ID)
	}
	return nil
}

func (h *Handler) finishCompensation(order *store.Order, log *slog.Logger) {
	reason := "saga compensation completed"
	event := &eventspb.OrderCancelled{
		EventId:       uuid.New().String(),
		OrderId:       order.ID,
		CustomerId:    order.CustomerID,
		Reason:        reason,
		OccurredAt:    timestamppb.Now(),
		CorrelationId: order.ID,
	}
	payload, err := proto.Marshal(event)
	if err != nil {
		log.Error("failed to marshal OrderCancelled", "err", err)
		return
	}
	_ = h.store.RecordStepResult(order.ID, saga.StepName(""), "backward", "SUCCEEDED", saga.StateCancelled, "OrderCancelled", payload, reason)
}
