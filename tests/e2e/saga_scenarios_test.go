package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func submitOrder(t *testing.T, idemKey, token, sku string) (*http.Response, string) {
	reqBody := OrderRequest{
		CustomerID:   "cust_e2e",
		Currency:     "USD",
		PaymentToken: token,
		Items: []OrderItem{
			{SKU: sku, Quantity: 1, UnitPriceCents: 1000},
		},
	}
	bodyBytes, _ := json.Marshal(reqBody)
	
	req, _ := http.NewRequest(http.MethodPost, "http://localhost:8080/v1/orders", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", idemKey)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	var orderID string
	if resp.StatusCode == http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		resp.Body = io.NopCloser(bytes.NewReader(body)) // restore body
		var orderResp OrderResponse
		json.Unmarshal(body, &orderResp)
		orderID = orderResp.OrderID
	}
	return resp, orderID
}

func waitForStatus(t *testing.T, orderID, expectedStatus string, timeout time.Duration) {
	require.Eventually(t, func() bool {
		r, err := http.Get(fmt.Sprintf("http://localhost:8080/v1/orders/%s", orderID))
		if err != nil {
			return false
		}
		defer r.Body.Close()
		
		body, _ := io.ReadAll(r.Body)
		var o map[string]interface{}
		json.Unmarshal(body, &o)
		
		status, _ := o["status"].(string)
		return status == expectedStatus
	}, timeout, 500*time.Millisecond, "order never reached %s state", expectedStatus)
}

func TestSaga_F4_StockUnavailable(t *testing.T) {
	idemKey := fmt.Sprintf("f4-stock-%d", time.Now().UnixNano())
	resp, orderID := submitOrder(t, idemKey, "tok_visa_ok", "SKU-SOLDOUT")
	require.Equal(t, http.StatusAccepted, resp.StatusCode)

	// Since we request 1 item but none exist (or rather, the DB doesn't have enough), it should fail reserve stock and cancel.
	// Wait, the default migration seeds inventory. Let's assume we ask for 1000 items to guarantee failure if SKU-SOLDOUT doesn't explicitly fail.
	// Actually, wait, the DB schema has SKU-1. If we pass SKU-SOLDOUT, the DB will return insufficient stock.
	waitForStatus(t, orderID, "CANCELLED", 60*time.Second)
}

func TestSaga_F5_PaymentDeclined(t *testing.T) {
	idemKey := fmt.Sprintf("f5-declined-%d", time.Now().UnixNano())
	resp, orderID := submitOrder(t, idemKey, "tok_declined", "SKU-1")
	require.Equal(t, http.StatusAccepted, resp.StatusCode)

	waitForStatus(t, orderID, "CANCELLED", 60*time.Second)
}

func TestSaga_F9_DuplicatePost(t *testing.T) {
	idemKey := fmt.Sprintf("f9-dup-%d", time.Now().UnixNano())
	resp1, orderID1 := submitOrder(t, idemKey, "tok_visa_ok", "SKU-1")
	require.Equal(t, http.StatusAccepted, resp1.StatusCode)

	waitForStatus(t, orderID1, "CONFIRMED", 15*time.Second)

	// Exact duplicate
	resp2, orderID2 := submitOrder(t, idemKey, "tok_visa_ok", "SKU-1")
	require.Equal(t, http.StatusAccepted, resp2.StatusCode)
	require.Equal(t, orderID1, orderID2, "duplicate POST should return the same order ID")

	// Same key, different body (change token)
	resp3, _ := submitOrder(t, idemKey, "tok_different", "SKU-1")
	require.Equal(t, http.StatusUnprocessableEntity, resp3.StatusCode, "should reject reused idempotency key with different payload")
}

func TestSaga_F3_OrchestratorCrash(t *testing.T) {
	require.NoError(t, runCmd("docker", "compose", "-f", "../../deploy/compose/docker-compose.yml", "stop", "orchestrator"))
	require.NoError(t, runCmdWithEnv([]string{"CARTWRIGHT_FAULTS=1"}, "docker", "compose", "-f", "../../deploy/compose/docker-compose.yml", "up", "-d", "orchestrator"))

	require.Eventually(t, func() bool {
		out, _ := runCmdOutput("docker", "inspect", "--format={{.State.Health.Status}}", "cartwright-orchestrator")
		return strings.Contains(out, "healthy")
	}, 60*time.Second, 1*time.Second, "orchestrator never became healthy")

	idemKey := fmt.Sprintf("f3-crash-%d", time.Now().UnixNano())
	
	// Create request with cust_orch_fault
	reqBody := OrderRequest{
		CustomerID:   "cust_orch_fault",
		Currency:     "USD",
		PaymentToken: "tok_visa_ok",
		Items: []OrderItem{
			{SKU: "SKU-1", Quantity: 1, UnitPriceCents: 1000},
		},
	}
	bodyBytes, _ := json.Marshal(reqBody)
	
	req, _ := http.NewRequest(http.MethodPost, "http://localhost:8080/v1/orders", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", idemKey)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusAccepted, resp.StatusCode)

	var orderResp OrderResponse
	body, _ := io.ReadAll(resp.Body)
	json.Unmarshal(body, &orderResp)
	resp.Body.Close()
	orderID := orderResp.OrderID

	// Wait for orchestrator to hit the fault hook
	require.Eventually(t, func() bool {
		logs, _ := runCmdOutput("docker", "logs", "cartwright-orchestrator")
		return strings.Contains(logs, "Orchestrator fault hook active")
	}, 15*time.Second, 500*time.Millisecond, "orchestrator never reached fault hook")

	// Kill orchestrator
	runCmd("docker", "kill", "cartwright-orchestrator")
	
	// Restart orchestrator without faults
	time.Sleep(2 * time.Second)
	runCmd("docker", "compose", "-f", "../../deploy/compose/docker-compose.yml", "up", "-d", "orchestrator")

	// The recovery loop should pick it up and finish it
	waitForStatus(t, orderID, "CONFIRMED", 60*time.Second)
}

func TestSaga_F8_NotifierDown(t *testing.T) {
	// Stop notifier
	runCmd("docker", "compose", "-f", "../../deploy/compose/docker-compose.yml", "stop", "notifier")

	idemKey := fmt.Sprintf("f8-notif-%d", time.Now().UnixNano())
	resp, orderID := submitOrder(t, idemKey, "tok_visa_ok", "SKU-1")
	require.Equal(t, http.StatusAccepted, resp.StatusCode)

	// Order still confirms even if notifier is down
	waitForStatus(t, orderID, "CONFIRMED", 15*time.Second)

	// Start notifier
	runCmd("docker", "compose", "-f", "../../deploy/compose/docker-compose.yml", "up", "-d", "notifier")

	// Verify notifier eventually processed it
	require.Eventually(t, func() bool {
		notifierLogs, _ := runCmdOutput("docker", "logs", "cartwright-notifier")
		return strings.Contains(notifierLogs, fmt.Sprintf("order %s confirmed", orderID))
	}, 60*time.Second, 1*time.Second, "notifier never logged the confirmed order")
}
