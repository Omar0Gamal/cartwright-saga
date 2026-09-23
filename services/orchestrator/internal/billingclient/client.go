package billingclient

import (
	"context"
	"math/rand"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	billingpb "github.com/Omar0Gamal/cartwright/services/orchestrator/gen/billing/v1"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
)

type AuthorizeResult struct {
	PaymentID     string
	Declined      bool
	DeclineReason string
}

type Client struct {
	client billingpb.BillingClient
}

func New(addr string) (*Client, error) {
	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
	)
	if err != nil {
		return nil, err
	}
	return &Client{client: billingpb.NewBillingClient(conn)}, nil
}

// Only retry codes that can plausibly succeed on a second attempt.
func isRetryable(err error) bool {
	st, ok := status.FromError(err)
	if !ok {
		return true // network-level error with no status — worth retrying
	}
	switch st.Code() {
	case codes.Unavailable, codes.DeadlineExceeded, codes.Aborted:
		return true
	default:
		return false
	}
}

func withRetry(maxAttempts int, maxDelay time.Duration, op func() error) error {
	var err error
	delay := 100 * time.Millisecond

	for i := 0; maxAttempts < 0 || i < maxAttempts; i++ {
		err = op()
		if err == nil {
			return nil
		}
		if !isRetryable(err) {
			return err
		}
		if maxAttempts >= 0 && i == maxAttempts-1 {
			break
		}
		jittered := time.Duration(rand.Int63n(int64(delay)))
		time.Sleep(jittered)
		delay *= 2
		if delay > maxDelay {
			delay = maxDelay
		}
	}
	return err
}

func (c *Client) Authorize(ctx context.Context, orderID, ik, token string, amountCents int64, currency string) (*AuthorizeResult, error) {
	var result *AuthorizeResult
	err := withRetry(10, 30*time.Second, func() error {
		reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		resp, callErr := c.client.Authorize(reqCtx, &billingpb.AuthorizeRequest{
			IdempotencyKey: ik,
			OrderId:        orderID,
			AmountCents:    amountCents,
			Currency:       currency,
			PaymentToken:   token,
		})
		if callErr != nil {
			return callErr
		}
		result = &AuthorizeResult{
			PaymentID:     resp.PaymentId,
			Declined:      resp.Status == billingpb.PaymentStatus_PAYMENT_STATUS_DECLINED,
			DeclineReason: resp.DeclineReason,
		}
		return nil
	})
	return result, err
}

func (c *Client) Capture(ctx context.Context, orderID, ik string) error {
	return withRetry(-1, 30*time.Second, func() error {
		reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		_, err := c.client.Capture(reqCtx, &billingpb.CaptureRequest{
			IdempotencyKey: ik,
			OrderId:        orderID,
		})
		return err
	})
}

// Void uses unlimited retries with a 30s backoff cap — compensations must eventually succeed.
func (c *Client) Void(ctx context.Context, orderID, ik string) error {
	return withRetry(-1, 30*time.Second, func() error {
		reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		_, err := c.client.Void(reqCtx, &billingpb.VoidRequest{
			IdempotencyKey: ik,
			OrderId:        orderID,
		})
		return err
	})
}
