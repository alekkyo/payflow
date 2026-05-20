# PayFlow — Distributed Payment Processing Platform
## Architecture & Context Document

---

## About the Developer

I am a Senior Software Engineer / Tech Lead with 10+ years of experience in:
- **PHP (Laravel)** — primary backend language
- **VueJS, React, React Native** — frontend
- **MySQL, PostgreSQL** — databases
- **REST APIs, MVC architecture** — patterns

I was recently laid off and am using this time to learn Go and distributed systems before starting my job search in August. I have completed 12 progressive Go exercises covering:

1. File I/O, maps, slices, sorting
2. Structs, methods, pointer receivers, fmt.Stringer
3. Interfaces, implicit satisfaction
4. Custom errors, errors.As, error wrapping
5. JSON encoding, struct tags
6. Table-driven tests
7. Goroutines, sync.WaitGroup, sync.Mutex
8. Channels, pipelines, context.WithCancel, select
9. HTTP REST API, middleware, net/http
10. PostgreSQL with pgx/v5 driver
11. Redis caching layer, cache-aside pattern
12. Worker pool, graceful shutdown, os/signal, log/slog

I am comfortable with Go fundamentals but this is my first large Go project. I am targeting a position at a **payments processing company**, which is why this project focuses heavily on payments engineering concepts.

---

## Project Goal

Build a production-grade, distributed e-commerce order and payment processing platform in Go. The project should demonstrate:

- Distributed systems patterns (saga, idempotency, optimistic locking, event sourcing)
- Payments engineering concepts (idempotency keys, webhook deduplication, reconciliation, partial refunds)
- Scalable architecture (horizontal scaling, queue-based workers, Redis caching)
- Observability (structured logging with slog, OpenTelemetry traces, Prometheus metrics)
- Modern Go practices (interfaces, error wrapping, context propagation, graceful shutdown)
- Containerization (Docker, Docker Compose)
- Cloud deployment (AWS ECS Fargate — planned after core is built)

This project will be public on GitHub and is the primary technical artifact for job interviews.

---

## Tech Stack

| Technology | Purpose |
|---|---|
| **Go 1.24** | Primary language — API service and worker service |
| **PostgreSQL 16** | Primary data store — orders, payments, products, audit log |
| **Redis 7** | Caching, distributed locks, rate limiting, Redis Streams queue |
| **Stripe** | Payment provider (test mode) |
| **Docker + Docker Compose** | Local development environment |
| **React 19** | Frontend — product catalog, checkout, order tracking, admin |
| **OpenTelemetry** | Distributed tracing |
| **Prometheus + Grafana** | Metrics and dashboards |
| **AWS ECS Fargate** | Deployment target (later phase) |

---

## Repository Structure

```
payflow/
├── cmd/
│   ├── api/              # API service entrypoint
│   │   └── main.go
│   └── worker/           # Worker service entrypoint
│       └── main.go
├── internal/
│   ├── api/              # HTTP handlers, middleware, routing
│   │   ├── handlers/
│   │   ├── middleware/
│   │   └── server.go
│   ├── domain/           # Core business logic, no external dependencies
│   │   ├── order/
│   │   ├── payment/
│   │   ├── product/
│   │   └── inventory/
│   ├── store/            # Database layer
│   │   ├── postgres/
│   │   └── redis/
│   ├── queue/            # Redis Streams producer/consumer
│   ├── worker/           # Worker implementations
│   │   ├── payment_worker.go
│   │   ├── inventory_worker.go
│   │   ├── email_worker.go
│   │   ├── refund_worker.go
│   │   └── reconcile_worker.go
│   ├── stripe/           # Stripe client wrapper
│   └── observability/    # OpenTelemetry, slog setup, Prometheus
├── migrations/           # SQL migration files
│   ├── 001_create_products.sql
│   ├── 002_create_inventory.sql
│   ├── 003_create_orders.sql
│   ├── 004_create_payments.sql
│   └── 005_create_reconciliation.sql
├── frontend/             # React 19 application
│   ├── src/
│   │   ├── pages/
│   │   ├── components/
│   │   └── hooks/
│   └── package.json
├── docker-compose.yml    # PostgreSQL, Redis, Stripe CLI, Grafana
├── docker-compose.prod.yml
├── Makefile              # make dev, make test, make migrate, make build
├── ARCHITECTURE.md       # This file
└── README.md
```

---

## System Architecture

```
┌─────────────────────────────────────────────────────────┐
│                    React Frontend                         │
│   Product catalog │ Checkout │ Order tracking │ Admin    │
└───────────────────────────┬─────────────────────────────┘
                            │ HTTPS + SSE
┌───────────────────────────▼─────────────────────────────┐
│                      Go API Service                       │
│                                                           │
│  /products    /cart    /orders    /payments    /webhooks  │
│  /admin       /metrics  /health   /events/stream         │
└──┬──────────┬──────────┬──────────┬────────────┬─────────┘
   │          │          │          │            │
   ▼          ▼          ▼          ▼            ▼
PostgreSQL  Redis      Redis      Stripe      OpenTelemetry
(source of  (cache,    Streams    (payment      (traces,
 truth)      locks,    (queue)     provider)     metrics)
             rate                                  │
             limit)                                ▼
                    ┌─────────────────────┐    Grafana /
                    │    Worker Service    │    Datadog
                    │                     │
                    │  payment_worker      │
                    │  inventory_worker    │
                    │  email_worker        │
                    │  refund_worker       │
                    │  reconcile_worker    │
                    └─────────────────────┘
```

---

## The Order & Payment Saga (Core Business Logic)

This is the most important part of the system. An order moves through a strict state machine. Each transition is immutable — written as an event to `order_events`, never updating the order row directly (event sourcing pattern).

### Happy Path

```
CART
  ↓ POST /orders (with idempotency key)
ORDER_CREATED
  ↓ inventory_worker: reserve stock (optimistic locking)
INVENTORY_RESERVED
  ↓ payment_worker: create Stripe PaymentIntent
PAYMENT_PROCESSING
  ↓ Stripe webhook: payment_intent.succeeded
PAYMENT_CAPTURED
  ↓ email_worker: send confirmation email
  ↓ analytics_worker: update revenue metrics
CONFIRMED
  ↓ fulfillment_worker: (simulated) ship order
FULFILLED
```

### Failure & Compensation (Saga Pattern)

```
If PAYMENT_FAILED:
  → refund_worker: (no charge yet, skip)
  → inventory_worker: release reserved stock
  → ORDER_CANCELLED

If INVENTORY_FAILED (out of stock):
  → (no payment attempted yet)
  → ORDER_CANCELLED
  → email_worker: notify customer — out of stock

If post-capture failure:
  → refund_worker: issue full Stripe refund
  → inventory_worker: release stock
  → ORDER_REFUNDED
```

---

## PostgreSQL Schema

### Products & Inventory

```sql
CREATE TABLE products (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL,
    description TEXT,
    price_cents INT NOT NULL,      -- always store money in cents, never floats
    currency    TEXT NOT NULL DEFAULT 'usd',
    active      BOOLEAN NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE inventory (
    product_id  UUID PRIMARY KEY REFERENCES products(id),
    quantity    INT NOT NULL DEFAULT 0,
    reserved    INT NOT NULL DEFAULT 0,   -- soft-reserved, not yet sold
    version     INT NOT NULL DEFAULT 0,   -- optimistic locking version
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT quantity_non_negative CHECK (quantity >= 0),
    CONSTRAINT reserved_non_negative CHECK (reserved >= 0),
    CONSTRAINT reserved_lte_quantity CHECK (reserved <= quantity)
);
```

### Orders & Event Log

```sql
CREATE TABLE orders (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL,
    status          TEXT NOT NULL DEFAULT 'created',
    total_cents     INT NOT NULL,
    currency        TEXT NOT NULL DEFAULT 'usd',
    idempotency_key TEXT NOT NULL UNIQUE,   -- prevents duplicate order creation
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE order_items (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id    UUID NOT NULL REFERENCES orders(id),
    product_id  UUID NOT NULL REFERENCES products(id),
    quantity    INT NOT NULL,
    price_cents INT NOT NULL,   -- snapshot of price at time of order
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Append-only event log — never update, only insert
-- Current order state is derived from the latest event
CREATE TABLE order_events (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id    UUID NOT NULL REFERENCES orders(id),
    event_type  TEXT NOT NULL,   -- 'created', 'inventory_reserved', 'payment_captured', etc.
    payload     JSONB,           -- flexible per-event data
    created_by  TEXT,            -- 'api', 'payment_worker', 'inventory_worker', etc.
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_order_events_order_id ON order_events(order_id);
CREATE INDEX idx_orders_user_id ON orders(user_id);
CREATE INDEX idx_orders_status ON orders(status);
```

### Payments

```sql
CREATE TABLE payments (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id          UUID NOT NULL REFERENCES orders(id),
    stripe_payment_id TEXT UNIQUE,          -- Stripe PaymentIntent ID
    amount_cents      INT NOT NULL,
    currency          TEXT NOT NULL DEFAULT 'usd',
    status            TEXT NOT NULL,        -- 'pending', 'processing', 'captured', 'failed', 'refunded'
    idempotency_key   TEXT NOT NULL UNIQUE, -- prevents duplicate payment creation
    failure_reason    TEXT,                 -- populated on failure
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Append-only payment audit log
CREATE TABLE payment_events (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    payment_id  UUID NOT NULL REFERENCES payments(id),
    event_type  TEXT NOT NULL,   -- 'initiated', 'captured', 'failed', 'refund_requested', 'refunded'
    provider    TEXT NOT NULL DEFAULT 'stripe',
    raw_payload JSONB,           -- full Stripe event body stored for audit/compliance
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Webhook deduplication — Stripe sends webhooks at least once
CREATE TABLE processed_webhook_events (
    event_id     TEXT PRIMARY KEY,   -- Stripe event ID (e.g. evt_xxx)
    event_type   TEXT NOT NULL,
    processed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE refunds (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    payment_id       UUID NOT NULL REFERENCES payments(id),
    amount_cents     INT NOT NULL,
    reason           TEXT,            -- 'customer_request', 'item_damaged', 'fraud', etc.
    stripe_refund_id TEXT UNIQUE,
    status           TEXT NOT NULL,   -- 'pending', 'succeeded', 'failed'
    idempotency_key  TEXT NOT NULL UNIQUE,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

### Reconciliation

```sql
CREATE TABLE reconciliation_runs (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_date      DATE NOT NULL UNIQUE,
    status        TEXT NOT NULL,   -- 'running', 'completed', 'failed'
    matched       INT NOT NULL DEFAULT 0,
    mismatched    INT NOT NULL DEFAULT 0,
    missing_local INT NOT NULL DEFAULT 0,   -- in Stripe but not in our DB
    missing_stripe INT NOT NULL DEFAULT 0,  -- in our DB but not in Stripe
    started_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at  TIMESTAMPTZ
);

CREATE TABLE reconciliation_discrepancies (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    reconciliation_id   UUID NOT NULL REFERENCES reconciliation_runs(id),
    payment_id          UUID REFERENCES payments(id),
    stripe_payment_id   TEXT,
    discrepancy_type    TEXT NOT NULL,   -- 'amount_mismatch', 'missing_local', 'missing_stripe', 'status_mismatch'
    our_amount_cents    INT,
    stripe_amount_cents INT,
    our_status          TEXT,
    stripe_status       TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

### Users & Auth

```sql
CREATE TABLE users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email         TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role          TEXT NOT NULL DEFAULT 'customer',   -- 'customer', 'admin'
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE sessions (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users(id),
    token_hash TEXT NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

---

## Redis Key Design

```
# Product cache
product:{uuid}              → JSON (TTL: 10 min)
products:catalog:page:{n}   → JSON array (TTL: 5 min)

# Order status cache (for SSE polling)
order:{uuid}:status         → string (TTL: 1 hr)

# Idempotency cache (fast check before hitting DB)
idempotency:payment:{key}   → JSON result (TTL: 24 hr)
idempotency:order:{key}     → JSON result (TTL: 24 hr)

# Distributed locks (using SET NX PX)
lock:inventory:{product_id} → "locked" (TTL: 5 sec)
lock:payment:{order_id}     → "locked" (TTL: 30 sec)

# Rate limiting (sliding window)
ratelimit:checkout:{user_id}   → counter (TTL: 1 min, max: 5)
ratelimit:webhook:{ip}         → counter (TTL: 1 min, max: 100)

# Redis Streams (queue)
stream:orders.created          → order saga starts here
stream:payments.ready          → inventory reserved, ready to charge
stream:payments.captured       → charge succeeded
stream:payments.failed         → charge failed, trigger compensation
stream:refunds.requested       → refund initiated
stream:stripe.webhooks         → raw Stripe webhook events
stream:reconciliation.trigger  → daily reconciliation job
stream:deadletter              → failed events after max retries
```

---

## Worker Pipeline Detail

### payment_worker
- Reads from `stream:payments.ready`
- Creates Stripe PaymentIntent
- Stores payment record with idempotency key
- Publishes to `stream:payments.captured` or `stream:payments.failed`
- On failure: exponential backoff retry (3 attempts), then dead letter

### inventory_worker
- Reads from `stream:orders.created`
- Uses optimistic locking (version column) to reserve stock
- Publishes to `stream:payments.ready` on success
- Publishes to `stream:orders.cancelled` on failure (out of stock)
- Also reads from `stream:payments.failed` to release reservations

### email_worker
- Reads from `stream:payments.captured`
- Sends order confirmation (simulated — log to stdout in dev)
- Idempotent — safe to retry

### refund_worker
- Reads from `stream:refunds.requested`
- Calls Stripe Refunds API with idempotency key
- Updates payment status and inserts refund record
- Releases inventory reservation

### reconcile_worker
- Triggered daily via `stream:reconciliation.trigger`
- Lists all payments from yesterday in PostgreSQL
- Lists all charges from yesterday via Stripe API
- Compares: amount, currency, status
- Writes discrepancies to `reconciliation_discrepancies`
- Alerts on mismatch count above threshold

---

## Key Payments Engineering Concepts to Implement

### 1. Idempotency Keys
Every payment and order creation endpoint requires an `Idempotency-Key` header. The server checks Redis first, then PostgreSQL, before processing. On duplicate request — return the original response without re-processing.

### 2. Optimistic Locking for Inventory
Use a `version` column on the `inventory` table. The UPDATE includes `WHERE version = $expected_version`. If another worker updated first, the UPDATE affects 0 rows — retry or fail gracefully. Never use `SELECT FOR UPDATE` (pessimistic locking) as it doesn't scale.

### 3. Webhook Signature Validation
All Stripe webhooks are validated using `stripe.ConstructEvent` with the webhook signing secret before any processing. Invalid signatures return 400 immediately.

### 4. Webhook Deduplication
After validation, check `processed_webhook_events` for the Stripe event ID. If already processed, return 200 immediately (Stripe will stop retrying). If new, insert the event ID and process asynchronously.

### 5. Always Respond Fast to Webhooks
Stripe expects a 200 response within 3 seconds. The webhook handler validates, deduplicates, enqueues to Redis Streams, and responds 200. All processing happens asynchronously in workers.

### 6. Money in Cents
All monetary values stored as integers (cents). Never use floats for money. `$19.99` is stored as `1999`. Displayed as formatted string on the frontend.

### 7. Partial Refunds
Refunds can be for specific line items or a custom amount. Each refund has its own idempotency key. Multiple partial refunds can exist for one payment as long as total refunded ≤ total charged.

### 8. Daily Reconciliation
A worker runs nightly comparing internal payment records against Stripe's API. Discrepancies are logged to the database and alerted on. This is a compliance requirement in real payment systems.

### 9. Append-Only Event Log
Never UPDATE `order_events` or `payment_events`. Only INSERT. Current state is derived from the event sequence. This provides a complete audit trail required for financial compliance.

---

## API Endpoints

### Auth
```
POST   /auth/register       → create account
POST   /auth/login          → get session token
POST   /auth/logout         → invalidate session
```

### Products
```
GET    /products            → list products (Redis cached)
GET    /products/:id        → get product (Redis cached)
POST   /products            → create product (admin only)
PUT    /products/:id        → update product (admin only)
DELETE /products/:id        → deactivate product (admin only)
```

### Cart & Orders
```
POST   /orders                      → create order (requires Idempotency-Key header)
GET    /orders                      → list my orders
GET    /orders/:id                  → get order with full event history
GET    /orders/:id/events/stream    → SSE — real-time order status updates
POST   /orders/:id/cancel           → cancel order (if not yet captured)
```

### Payments
```
GET    /payments/:id                → get payment details
POST   /orders/:id/refunds          → request refund (requires Idempotency-Key)
GET    /orders/:id/refunds          → list refunds for order
```

### Webhooks
```
POST   /webhooks/stripe             → Stripe webhook receiver
```

### Admin
```
GET    /admin/orders                → list all orders with filters
GET    /admin/payments              → list all payments
GET    /admin/reconciliation        → list reconciliation runs
GET    /admin/reconciliation/:id    → view discrepancies for a run
POST   /admin/reconciliation/trigger → trigger manual reconciliation
GET    /admin/metrics               → internal metrics summary
```

### Observability
```
GET    /health                      → liveness check
GET    /ready                       → readiness check (DB + Redis connected)
GET    /metrics                     → Prometheus metrics endpoint
```

---

## Observability Setup

### Structured Logging (slog)
Every significant operation logs with consistent fields:
```go
slog.Info("payment captured",
    "trace_id",      traceID,
    "order_id",      orderID,
    "payment_id",    paymentID,
    "amount_cents",  amountCents,
    "currency",      currency,
    "stripe_id",     stripePaymentID,
    "duration_ms",   duration.Milliseconds(),
    "worker_id",     workerID,
)
```

### Prometheus Metrics
```
# Counters
payflow_payments_total{status="captured|failed|refunded"}
payflow_orders_total{status="created|confirmed|cancelled|fulfilled"}
payflow_webhook_events_total{type="payment_intent.succeeded|..."}
payflow_reconciliation_discrepancies_total{type="amount_mismatch|..."}

# Histograms
payflow_payment_processing_duration_seconds
payflow_api_request_duration_seconds{method, path, status}
payflow_queue_processing_duration_seconds{stream, worker}

# Gauges
payflow_queue_depth{stream="orders.created|payments.ready|..."}
payflow_inventory_reserved{product_id}
```

### OpenTelemetry Traces
Every order traces end-to-end across API → queue → workers:
```
Trace: POST /orders
  └── span: validate_request
  └── span: check_idempotency
  └── span: create_order (postgres)
  └── span: enqueue_event (redis)
      └── span: inventory_worker.reserve
          └── span: postgres.update_inventory
          └── span: enqueue_payment_ready
              └── span: payment_worker.charge
                  └── span: stripe.create_payment_intent
                  └── span: postgres.update_payment
```

---

## React Frontend Scope

Minimal but complete — enough to show the full user flow:

### Pages
- `/` — Product catalog with search and filtering
- `/products/:id` — Product detail with add to cart
- `/cart` — Cart review
- `/checkout` — Stripe Elements card input + place order
- `/orders` — My orders list
- `/orders/:id` — Order detail with real-time status via SSE
- `/admin` — Admin dashboard (orders, payments, reconciliation)

### Tech
- React 19
- TanStack Query (data fetching, caching)
- React Router v7
- Tailwind CSS
- Stripe.js + React Stripe Elements (PCI-compliant card collection)

---

## Local Development Setup

### docker-compose.yml services
- `postgres` — PostgreSQL 16 on port 5432
- `redis` — Redis 7 on port 6379
- `stripe-cli` — Stripe CLI for local webhook forwarding
- `prometheus` — metrics collection
- `grafana` — dashboards on port 3000

### Makefile commands
```makefile
make dev          # start all Docker services
make api          # run Go API service
make worker       # run Go worker service
make migrate      # run database migrations
make test         # run all tests
make test-race    # run tests with race detector
make lint         # golangci-lint
make build        # build both binaries
make stripe-listen # forward Stripe webhooks to localhost
```

---

## Week-by-Week Build Plan

### Week 1 — Foundation
- Project structure, Go modules, Makefile
- Docker Compose (PostgreSQL, Redis, Stripe CLI, Prometheus, Grafana)
- Database migrations (all tables)
- Auth — register, login, JWT middleware
- Health and readiness endpoints
- Structured logging setup (slog)

### Week 2 — Products & Inventory
- Products CRUD API (admin)
- Inventory management with optimistic locking
- Redis caching for product catalog
- Distributed lock implementation (Redis SET NX)
- Unit tests for inventory reservation logic

### Week 3 — Orders & Saga
- Order creation with idempotency keys
- Redis Streams queue setup
- Inventory worker (reserve stock, publish next event)
- Order state machine and event log
- SSE endpoint for real-time order status

### Week 4 — Payments
- Stripe client wrapper
- Payment worker (create PaymentIntent, handle result)
- Stripe webhook handler (validate signature, deduplicate, enqueue)
- Refund worker
- Idempotency key implementation end-to-end
- Compensation logic (saga failure paths)

### Week 5 — Observability & Hardening
- OpenTelemetry trace propagation across API and workers
- Prometheus metrics on all key operations
- Grafana dashboards
- Reconciliation worker
- Dead letter queue handling
- Rate limiting middleware
- Integration tests for the full saga flow

### Week 6 — Frontend & Deployment
- React app — catalog, checkout, order tracking, admin
- Stripe.js integration for card collection
- Docker multi-stage builds for both Go binaries
- AWS ECS Fargate deployment (or Railway/Render for simplicity)
- README with architecture diagram
- Final cleanup and documentation

---

## Important Go Patterns to Use Throughout

### Error wrapping (always add context)
```go
return fmt.Errorf("payment_worker.charge order %s: %w", orderID, err)
```

### Context propagation (every DB/Redis/HTTP call)
```go
func (s *PaymentService) Capture(ctx context.Context, ...) error { ... }
```

### Graceful shutdown (both API and worker)
```go
ctx, cancel := context.WithCancel(context.Background())
signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
```

### Interface-driven design (easy to test and swap)
```go
type PaymentProvider interface {
    CreatePaymentIntent(ctx context.Context, req PaymentIntentRequest) (PaymentIntent, error)
    CreateRefund(ctx context.Context, req RefundRequest) (Refund, error)
}
// Stripe implements PaymentProvider
// MockPaymentProvider implements PaymentProvider (for tests)
```

### Table-driven tests for business logic
```go
// Test the saga state machine, idempotency, inventory locking
```

---

## Coding Preferences & Style

- **Error handling:** always wrap with context using `fmt.Errorf("operation: %w", err)`
- **No panics** in production code — return errors explicitly
- **Consistency:** use `return` not `os.Exit` except in `main()`
- **Naming:** follow Go conventions — short receiver names, unexported internals
- **Comments:** exported functions and types get doc comments
- **Tests:** table-driven, one concern per test, fresh dependencies per test case
- **No ORM** — write SQL directly using pgx, keep queries in the store layer
- **Money:** always `int` (cents), never `float64`
- **UUIDs:** use `gen_random_uuid()` in PostgreSQL, `uuid` type in Go structs
- **Timestamps:** always `TIMESTAMPTZ` (with timezone) in PostgreSQL
