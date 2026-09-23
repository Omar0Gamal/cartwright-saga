package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type OrderRequest struct {
	CustomerID   string      `json:"customer_id"`
	Currency     string      `json:"currency"`
	PaymentToken string      `json:"payment_token"`
	Items        []OrderItem `json:"items"`
}

type OrderItem struct {
	SKU            string `json:"sku"`
	Quantity       int    `json:"quantity"`
	UnitPriceCents int64  `json:"unit_price_cents"`
}

type OrderResponse struct {
	OrderID string `json:"order_id"`
	Status  string `json:"status"`
}

func TestSagaRecovery_BillingCrash(t *testing.T) {
	// 1. Enable fault hooks in billing and restart it
	require.NoError(t, runCmd("docker", "compose", "-f", "../../deploy/compose/docker-compose.yml", "stop", "billing"))
	require.NoError(t, runCmdWithEnv([]string{"CARTWRIGHT_FAULTS=1"}, "docker", "compose", "-f", "../../deploy/compose/docker-compose.yml", "up", "-d", "billing"))
	
	// Wait for billing to be healthy
	require.Eventually(t, func() bool {
		out, _ := runCmdOutput("docker", "inspect", "--format={{.State.Health.Status}}", "cartwright-billing")
		return strings.Contains(out, "healthy")
	}, 60*time.Second, 1*time.Second, "billing never became healthy")

	// 2. Submit order with tok_fault
	reqBody := OrderRequest{
		CustomerID:   "cust_fault",
		Currency:     "USD",
		PaymentToken: "tok_fault",
		Items: []OrderItem{
			{SKU: "SKU-1", Quantity: 1, UnitPriceCents: 1000},
		},
	}
	bodyBytes, _ := json.Marshal(reqBody)
	
	idemKey := fmt.Sprintf("fault-demo-%d", time.Now().UnixNano())
	req, _ := http.NewRequest(http.MethodPost, "http://localhost:8080/v1/orders", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", idemKey)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusAccepted, resp.StatusCode)

	var orderResp OrderResponse
	json.NewDecoder(resp.Body).Decode(&orderResp)
	resp.Body.Close()
	
	orderID := orderResp.OrderID
	t.Logf("Submitted order ID: %s", orderID)

	// 3. Wait for billing to log the fault hook
	require.Eventually(t, func() bool {
		logs, _ := runCmdOutput("docker", "logs", "cartwright-billing")
		return strings.Contains(logs, "Fault hook active")
	}, 15*time.Second, 500*time.Millisecond, "billing never reached fault hook")

	// 4. Kill billing container!
	t.Log("Killing billing container...")
	require.NoError(t, runCmd("docker", "kill", "cartwright-billing"))
	
	// 5. Restart billing without fault hooks
	time.Sleep(2 * time.Second)
	require.NoError(t, runCmd("docker", "compose", "-f", "../../deploy/compose/docker-compose.yml", "up", "-d", "billing"))

	// 6. Wait for orchestrator to recover and complete the saga
	t.Log("Waiting for saga recovery...")
	var finalStatus string
	require.Eventually(t, func() bool {
		r, err := http.Get(fmt.Sprintf("http://localhost:8080/v1/orders/%s", orderID))
		if err != nil {
			return false
		}
		defer r.Body.Close()
		
		body, _ := io.ReadAll(r.Body)
		var o map[string]interface{}
		json.Unmarshal(body, &o)
		
		finalStatus, _ = o["status"].(string)
		return finalStatus == "CONFIRMED"
	}, 30*time.Second, 1*time.Second, "order never reached CONFIRMED state")

	// 7. Assert exactly 1 captured payment in DB
	capturedCountStr, _ := runCmdOutput("docker", "compose", "-f", "../../deploy/compose/docker-compose.yml", "exec", "-T", "postgres", "psql", "-U", "postgres", "-d", "billing", "-t", "-c", 
		fmt.Sprintf("SELECT COUNT(*) FROM \"Payments\" WHERE \"OrderId\" = '%s' AND \"Status\" = 'Captured';", orderID))
	require.Equal(t, "1", strings.TrimSpace(capturedCountStr), "Expected exactly 1 captured payment")

	// 8. Assert notification sent
	time.Sleep(2 * time.Second)
	notifierLogs, _ := runCmdOutput("docker", "logs", "cartwright-notifier")
	require.Contains(t, notifierLogs, fmt.Sprintf("order %s confirmed", orderID), "Notifier should have logged the confirmed order")
}

func runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	return cmd.Run()
}

func runCmdWithEnv(env []string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Env = append(cmd.Environ(), env...)
	return cmd.Run()
}

func runCmdOutput(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}
