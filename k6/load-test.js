import http from 'k6/http';
import { check, sleep } from 'k6';
import { Trend } from 'k6/metrics';

const BASE_URL = __ENV.BASE_URL || 'https://payflow.alexkua.com/api';
const EMAIL    = __ENV.EMAIL    || 'customer@payflow.dev';
const PASSWORD = __ENV.PASSWORD || 'demo-customer-123';

const orderLatency = new Trend('order_creation_ms', true);

export const options = {
  stages: [
    { duration: '30s', target: 10 },  // warm up
    { duration: '2m',  target: 50 },  // sustained load
    { duration: '30s', target: 0  },  // ramp down
  ],
  thresholds: {
    // p99 order creation must stay under 3 s
    order_creation_ms: ['p(99)<3000'],
    // fewer than 5 % of all HTTP requests fail
    http_req_failed: ['rate<0.05'],
  },
};

// setup() runs once before any VU starts.
// Returns shared data (token + product IDs) injected into every iteration.
export function setup() {
  const loginRes = http.post(
    `${BASE_URL}/auth/login`,
    JSON.stringify({ email: EMAIL, password: PASSWORD }),
    { headers: { 'Content-Type': 'application/json' } },
  );
  check(loginRes, { 'login 200': (r) => r.status === 200 });

  const token = loginRes.json('token');
  if (!token) {
    throw new Error(`login failed: ${loginRes.status} ${loginRes.body}`);
  }

  const productsRes = http.get(`${BASE_URL}/products`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  check(productsRes, { 'products 200': (r) => r.status === 200 });

  const ids = (productsRes.json('products') || []).map((p) => p.id);
  if (ids.length === 0) {
    throw new Error('no products found — run `make seed` first');
  }

  return { token, productIds: ids };
}

export default function (data) {
  const { token, productIds } = data;

  // Pick a random product to spread load across products and avoid
  // a single product's inventory becoming the bottleneck.
  const productId = productIds[Math.floor(Math.random() * productIds.length)];

  const payload = JSON.stringify({
    items: [{ product_id: productId, quantity: 1 }],
  });

  const before = Date.now();
  const res = http.post(`${BASE_URL}/orders`, payload, {
    headers: {
      'Content-Type':   'application/json',
      Authorization:    `Bearer ${token}`,
      // Each VU+iteration pair produces a globally unique key.
      'Idempotency-Key': `k6-vu${__VU}-iter${__ITER}`,
    },
  });
  orderLatency.add(Date.now() - before);

  check(res, {
    'order created (201)': (r) => r.status === 201,
    'has order id':        (r) => !!r.json('id'),
  });

  // 500 ms think time keeps requests realistic and prevents the inventory
  // table from being hammered faster than the workers can release stock.
  sleep(0.5);
}
