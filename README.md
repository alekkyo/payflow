# PayFlow — Distributed Payment Processing Platform

A production-grade e-commerce and payments backend built in Go as a capstone project for learning distributed systems and payments engineering. Built over 6 weeks by a senior engineer transitioning from PHP/Laravel to Go.

**Live demo:** [payflow.alexkua.com](https://payflow.alexkua.com)  
**Demo credentials:** Admin `admin@payflow.dev` / `demo-admin-123` · Customer `customer@payflow.dev` / `demo-customer-123`

---

## What it does

PayFlow is a full-stack order and payment processing system. A customer browses products, adds them to a cart, and places an order. The order then flows through a distributed saga:

1. **Inventory reserved** — optimistic locking prevents overselling
2. **Payment initiated** — Stripe PaymentIntent created with a deterministic idempotency key
3. **Webhook received** — Stripe confirms payment; worker advances order to confirmed → fulfilled
4. **Real-time updates** — browser receives each state transition via Server-Sent Events

The admin dashboard shows queue depths, dead-letter messages, and daily reconciliation runs comparing local payments against Stripe's records.

---

## Architecture

```
┌──────────────────────────────────────────────────────────┐
│                    React 19 Frontend                      │
│   Product catalog  │  Checkout  │  Order tracking  │ Admin │
└────────────────────────────┬─────────────────────────────┘
                             │ HTTPS + SSE
┌────────────────────────────▼─────────────────────────────┐
│                      Go API Service                        │
│  /products  /orders  /payments  /webhooks  /admin  /metrics│
└──┬──────────┬─────────────┬────────────┬──────────────────┘
   │          │             │            │
   ▼          ▼             ▼            ▼
PostgreSQL  Redis        Stripe      OpenTelemetry
(source of  (cache,      (payment    (traces → Jaeger
 truth)      locks,       provider)   metrics → Prometheus
             streams)                 → Grafana)
                    ┌──────────────────────┐
                    │    Go Worker Service  │
                    │  inventory_worker     │
                    │  payment_worker       │
                    │  webhook_worker       │
                    │  refund_worker        │
                    │  reconcile_worker     │
                    └──────────────────────┘
```

---

## Technical highlights

### Distributed Saga pattern
Orders move through a strict state machine: `created → inventory_reserved → payment_processing → payment_captured → confirmed → fulfilled`. Each transition is published to a Redis Streams queue and processed by a dedicated worker. If any step fails, compensation runs in reverse (inventory released, order cancelled). No distributed transaction needed — each step is independently idempotent.

### Idempotency at every layer
Every order creation and payment request requires an `Idempotency-Key` header. The same key always produces the same result with no side-effects on retry — checked first in Redis (fast path), then PostgreSQL. The Stripe call uses a deterministic key (`payment:{order_id}`) so even a crash between creating the DB row and calling Stripe is safe to retry.

### Optimistic locking for inventory
The `inventory` table has a `version` column. Reserving stock uses `UPDATE ... WHERE version = $expected AND (quantity - reserved) >= $requested`. If another worker updated first, 0 rows are affected — retry up to 3 times, then fail with `ErrVersionConflict`. No `SELECT FOR UPDATE`, no distributed locks for inventory — this scales horizontally.

### Event sourcing for the order audit log
`order_events` is append-only — every state transition is INSERTed, never UPDATEd. The full history of an order (who changed it, when, with what payload) is always queryable. This satisfies financial audit requirements. Same pattern for `payment_events`.

### Webhook deduplication
Stripe sends webhooks at least once. After validating the signature with `stripe.ConstructEvent`, the handler checks `processed_webhook_events` for the Stripe event ID. If found: return 200 immediately. If new: insert the ID and enqueue the event to Redis Streams for async processing. The API responds in <5ms; the webhook worker does the heavy lifting.

### Daily reconciliation
A `reconcile_worker` compares every local payment record against Stripe's API for the same day. Discrepancies (missing on either side, amount mismatch, status drift from a missed webhook) are written to `reconciliation_discrepancies` and surfaced in the admin dashboard. This is a compliance requirement in real payment systems.

### Real-time order tracking via SSE
`GET /orders/:id/events/stream` holds an HTTP connection open and pushes `event: status` messages via Server-Sent Events. The worker publishes each status transition to a Redis Pub/Sub channel; the SSE handler forwards it to the browser instantly. No polling, no WebSocket handshake.

### Distributed lock for payment deduplication
Before creating a Stripe PaymentIntent, the payment worker acquires a per-order lock with `SET NX PX` in Redis. If two workers race to charge the same order, only one proceeds. The lock TTL is 30 seconds — longer than the Stripe API timeout.

---

## Payments engineering concepts

| Concept | Where implemented |
|---|---|
| Idempotency keys | `POST /orders`, `POST /payments`, Stripe API calls |
| Optimistic locking | `inventory` table version column |
| Webhook deduplication | `processed_webhook_events` table |
| Append-only audit log | `order_events`, `payment_events` |
| Money in cents | All `*_cents` columns are `INT`, never `FLOAT` |
| Partial refunds | `refunds` table, separate idempotency key per refund |
| Saga compensation | `inventory_worker` releases stock on payment failure |
| Daily reconciliation | `reconcile_worker` vs Stripe ListPaymentIntents |
| Distributed locking | Redis `SET NX` in `payment_worker`, `inventory_worker` |
| Webhook fast response | Validate → enqueue → 200 in <5ms |

---

## Tech stack

| Layer | Technology |
|---|---|
| API + Workers | Go 1.24 |
| Database | PostgreSQL 16 |
| Cache / Queue / Locks | Redis 7 (Streams, Pub/Sub, SET NX) |
| Payment provider | Stripe (test mode) |
| Frontend | React 19, TanStack Query, React Router v7, Tailwind CSS |
| Observability | OpenTelemetry traces, Prometheus metrics, Grafana dashboards |
| Containerisation | Docker + Docker Compose |
| Deployment | DigitalOcean droplet, Nginx, Let's Encrypt |

---

## Go patterns used

**Interface-driven design** — every external dependency (`payment.Store`, `payment.PaymentProvider`, `product.InventoryStore`) is an interface. The production implementation uses Stripe/PostgreSQL; tests use inline mock structs — no mock library required.

**Error wrapping with context** — every error is wrapped with `fmt.Errorf("operation name: %w", err)` so stack traces are readable without a debugger.

**Context propagation** — every function that touches a database, Redis, or external API takes `context.Context` as its first argument. Cancellations and timeouts propagate automatically.

**Graceful shutdown** — both API and worker listen for `SIGINT`/`SIGTERM` via `signal.NotifyContext`. The API drains in-flight requests; workers finish the current message before stopping.

**Table-driven tests** — all business logic tests follow the same pattern: define inputs and expected outputs as a slice of structs, loop with `t.Run`. Each case gets a fresh mock with injected behaviour.

---

## Project structure

```
payflow/
├── cmd/
│   ├── api/         # API entrypoint
│   ├── worker/      # Worker entrypoint
│   ├── migrate/     # Database migration runner
│   └── seed/        # Demo data seeder
├── internal/
│   ├── api/
│   │   ├── handlers/    # HTTP handlers (auth, orders, payments, products, admin)
│   │   ├── middleware/  # Auth, CORS, rate limiting, logging
│   │   └── server.go    # Router and server setup
│   ├── domain/
│   │   ├── order/       # Order types, state machine, Store interface
│   │   ├── payment/     # Payment types, Store + PaymentProvider interfaces
│   │   ├── product/     # Product + InventoryService with optimistic locking
│   │   ├── reconciliation/
│   │   └── user/
│   ├── worker/
│   │   ├── inventory_worker.go   # Reserve/release stock
│   │   ├── payment_worker.go     # Create Stripe PaymentIntent
│   │   ├── webhook_worker.go     # Process Stripe events
│   │   ├── refund_worker.go      # Issue Stripe refunds
│   │   └── reconcile_worker.go   # Daily reconciliation
│   ├── store/
│   │   ├── postgres/    # pgx/v5 implementations of all Store interfaces
│   │   └── redis/       # Product cache, distributed lock
│   ├── queue/           # Redis Streams producer/consumer
│   ├── stripe/          # Stripe client wrapper
│   └── observability/   # slog setup, OTel tracing, Prometheus metrics
├── migrations/          # SQL migration files (golang-migrate)
├── frontend/            # React 19 app
├── Dockerfile.api
├── Dockerfile.worker
├── docker-compose.yml       # Local dev (postgres, redis, grafana, jaeger)
├── docker-compose.prod.yml  # Production (api, worker, postgres, redis)
└── nginx/payflow.conf       # Nginx reverse proxy config
```

---

## Local development

**Prerequisites:** Go 1.24+, Docker, Node.js 20+

```bash
# 1. Start infrastructure
make dev            # postgres, redis, prometheus, grafana, jaeger

# 2. Run migrations and seed demo data
make migrate
make seed

# 3. Start the API and worker (separate terminals)
make api
make worker

# 4. Start the frontend dev server
make frontend-install
make frontend       # http://localhost:5173

# 5. Forward Stripe webhooks (requires Stripe CLI)
make stripe-listen
```

Copy `.env.example` to `.env` and set your Stripe test keys.

---

## Testing

```bash
make test           # all tests
make test-race      # with race detector
make lint           # golangci-lint
```

Tests use inline mock structs implementing domain interfaces — no external mock library. Key test coverage:

- `internal/domain/product` — inventory reservation, optimistic locking, version conflicts
- `internal/worker` — saga state transitions, idempotency guards, reconciliation algorithm, status mapping
- `internal/api/middleware` — auth token extraction, session validation, claims propagation

---

## Background

This project was built in 6 weeks as a capstone for learning Go and distributed systems. I am a senior engineer with 10+ years in PHP/Laravel, Vue.js, and React, targeting a position at a payments or fintech company. The goal was to demonstrate that production-grade Go and payments engineering concepts could be learned and applied quickly.

Topics progressively mastered before starting this project: interfaces, goroutines, channels, context, pgx, Redis, worker pools, and graceful shutdown. This codebase is the first large Go project.

See [ARCHITECTURE.md](./ARCHITECTURE.md) for the full system design and [DEPLOY.md](./DEPLOY.md) for deployment instructions.
