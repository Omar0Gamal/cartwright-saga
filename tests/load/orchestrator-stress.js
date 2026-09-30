import http from 'k6/http';
import { check, sleep } from 'k6';

export const options = {
  stages: [
    { duration: '30s', target: 20 },
    { duration: '1m',  target: 20 },
    { duration: '30s', target: 0 },
  ],
  thresholds: {
    http_req_duration: ['p(95)<500'],
    http_req_failed: ['rate<0.01'],
  },
};

export default function () {
  const idempotencyKey = `k6-test-${__VU}-${__ITER}-${Date.now()}`;

  const payload = JSON.stringify({
    customer_id: 'cust-12345',
    items: [{ product_id: 'prod-abc', quantity: 1 }],
    amount: 100,
  });

  const params = {
    headers: {
      'Content-Type': 'application/json',
      'Idempotency-Key': idempotencyKey,
    },
  };

  const url = __ENV.ORCHESTRATOR_URL || 'http://localhost:8080/v1/orders';
  const res = http.post(url, payload, params);

  check(res, {
    'status 202': (r) => r.status === 202,
    'has order_id': (r) => JSON.parse(r.body).order_id !== undefined,
  });

  sleep(1);
}
