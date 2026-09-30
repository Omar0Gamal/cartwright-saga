import http from 'k6/http';
import { check, sleep } from 'k6';

export const options = {
  stages: [
    { duration: '30s', target: 20 },  // Ramp-up to 20 users over 30s
    { duration: '1m', target: 20 },   // Stay at 20 users for 1m
    { duration: '30s', target: 0 },   // Ramp-down to 0 users
  ],
  thresholds: {
    http_req_duration: ['p(95)<500'], // 95% of requests must complete below 500ms
    http_req_failed: ['rate<0.01'],   // Error rate must be less than 1%
  },
};

export default function () {
  // Use a dynamic idempotency key for each request
  const idempotencyKey = `k6-test-${__VU}-${__ITER}-${Date.now()}`;
  
  const payload = JSON.stringify({
    customer_id: "cust-12345",
    items: [
      { product_id: "prod-abc", quantity: 1 }
    ],
    amount: 100
  });

  const params = {
    headers: {
      'Content-Type': 'application/json',
      'Idempotency-Key': idempotencyKey,
    },
  };

  // Assuming orchestrator is exposed via Ingress or port-forward at localhost:8080
  const url = __ENV.ORCHESTRATOR_URL || 'http://localhost:8080/v1/orders';
  
  const res = http.post(url, payload, params);

  check(res, {
    'is status 202': (r) => r.status === 202,
    'has order id': (r) => JSON.parse(r.body).order_id !== undefined,
  });

  sleep(1);
}
