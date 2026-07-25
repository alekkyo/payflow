# PayFlow — Distributed Payment Processing Platform

A production-grade e-commerce and payments backend built in Go as a capstone project for learning distributed systems and payments engineering. Built over 2 weeks by a senior engineer with no prior Go experience, transitioning from PHP/Laravel.

**Live demo:** [payflow.alexkua.com](https://payflow.alexkua.com)

| Role | Email | Password |
|---|---|---|
| Admin | `admin@payflow.dev` | `demo-admin-123` |
| Customer | `customer@payflow.dev` | `demo-customer-123` |

---

## What it does

PayFlow is a full-stack order and payment processing system. A customer browses products, adds them to a cart, and places an order. The order then flows through a distributed saga:

1. **Inventory reserved** — optimistic locking prevents overselling
2. **Payment initiated** — Stripe PaymentIntent created with a deterministic idempotency key
3. **Webhook received** — Stripe confirms payment; worker advances order to confirmed → fulfilled
4. **Real-time updates** — browser receives each state transition via Server-Sent Events

The admin dashboard shows queue depths, dead-letter messages, daily reconciliation runs, and an AI-generated payment insights panel powered by Claude.

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

### AI payment insights
`GET /admin/insights` aggregates the last 7 days of payment data from PostgreSQL (volume, success rate, failure rate, refund rate, average transaction size) and the most recent reconciliation run results, then sends a structured prompt to Claude (`claude-sonnet-5`). Claude returns a plain-English narrative and a list of anomalies (e.g. velocity spikes, duplicate IP clusters). Results are cached in Redis for 30 minutes; pass `?refresh=true` to bypass the cache and call Claude immediately. The endpoint is admin-only and degrades gracefully if `ANTHROPIC_API_KEY` is absent.

### Frontend design system (Organic)
The React frontend uses a custom design system called **Organic** built on Tailwind CSS v3. Design tokens are defined in `tailwind.config.js`:
- **Colors:** terracotta accent (`#c67139`), sage accent (`#7a8a5e`), warm cream background (`#f5ead8`)
- **Typography:** Caprasimo (headings) + Figtree (body), loaded via Google Fonts
- **Radii/shadows:** three-step scale (`pf-sm` 8px → `pf-md` 16px → `pf-lg` 28px) with ink-tinted shadows
- **Animations:** `pf-pop`, `pf-pulse`, `pf-spin`, `pf-shimmer`, `pf-fade` — defined at CSS root level so inline `style={{ animation }}` references work regardless of Tailwind's `@layer` wrapping

All color, font, and dimension values that proved unreliable as Tailwind arbitrary variants are applied via explicit inline styles.

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
| API + Workers | Go 1.26 |
| Database | PostgreSQL 16 |
| Cache / Queue / Locks | Redis 7 (Streams, Pub/Sub, SET NX) |
| Payment provider | Stripe (test mode) |
| Frontend | React 19, TanStack Query, React Router v7, Tailwind CSS v3 |
| UI design system | Organic — Caprasimo + Figtree fonts, terracotta/sage palette |
| AI insights | Anthropic Claude (`claude-sonnet-5`), cached in Redis |
| Observability | OpenTelemetry traces, Prometheus metrics, Grafana dashboards |
| Containerisation | Docker + Docker Compose |
| CI/CD | GitHub Actions → GHCR → auto-deploy on merge to main |
| Lint | golangci-lint v2 (`errcheck`, `staticcheck`, `ineffassign`) |
| Load testing | k6 — 20 VUs, p99 78 ms, 0 % error rate over 3,388 orders |
| Deployment | Nginx, Let's Encrypt SSL, pre-built images from GHCR |

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
│   ├── insights/        # AI payment insights (Claude API + Redis cache)
│   ├── queue/           # Redis Streams producer/consumer
│   ├── stripe/          # Stripe client wrapper
│   └── observability/   # slog setup, OTel tracing, Prometheus metrics
├── migrations/          # SQL migration files (golang-migrate)
├── frontend/            # React 19 app
├── .github/
│   └── workflows/
│       ├── ci.yml           # Lint + test + build on every push and PR
│       └── deploy.yml       # Build images → push GHCR → SSH deploy on main
├── .golangci.yml            # golangci-lint v2 configuration
├── k6/
│   └── load-test.js         # k6 load test — login → place orders → measure latency
├── Dockerfile.api
├── Dockerfile.worker
├── docker-compose.yml       # Local dev (postgres, redis, grafana, jaeger)
├── docker-compose.prod.yml  # Production (pulls pre-built images from GHCR)
└── nginx/payflow.conf       # Nginx reverse proxy config
```

---

## Local development

**Prerequisites:** Go 1.26+, Docker, Node.js 20+

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

## CI/CD

Every push triggers the CI workflow automatically. Every merge to `main` triggers a full deploy.

```
Push to any branch
  → CI: golangci-lint → go test -race → go build → frontend tsc + vite build

Merge to main (CI passes)
  → Deploy: build Docker images → push to GHCR → SSH to server → docker compose pull + up
```

Two workflow files in `.github/workflows/`:

- **`ci.yml`** — runs on every push and PR. Go and frontend jobs run in parallel. Uses `go-version-file: go.mod` so the Go version is always in sync with the module.
- **`deploy.yml`** — triggered by `workflow_run` on CI, so it only fires when CI passes. Uses Docker layer cache (`type=gha`) so unchanged layers (e.g. `go mod download`) are restored from cache — rebuilds take ~1 minute instead of 5+.

Images are tagged with both `:latest` and `:<git-sha>` on every deploy, so any commit can be rolled back to by updating the tag.

---

## Testing

```bash
make test           # all tests
make test-race      # with race detector (detects concurrent memory access bugs)
make lint           # golangci-lint v2
```

Tests use inline mock structs implementing domain interfaces — no external mock library. Key test coverage:

- `internal/domain/product` — inventory reservation, optimistic locking, version conflicts
- `internal/worker` — saga state transitions, idempotency guards, reconciliation algorithm, status mapping
- `internal/api/middleware` — auth token extraction, session validation, claims propagation

---

## Load testing

`k6/load-test.js` runs a realistic end-to-end scenario: login → fetch products → place orders under 20 concurrent virtual users (ramp 30 s → hold 2 min → ramp down 30 s).

**Results (production, 20 VUs, 3 min):**

| Metric | Value |
|---|---|
| Total orders placed | 3,388 |
| Sustained throughput | 18.75 req/s |
| Order creation p50 | 39 ms |
| Order creation p95 | 52 ms |
| Order creation p99 | 78 ms |
| HTTP error rate | 0 % |

Both thresholds passed: p99 < 3 s and error rate < 5 %.

```bash
brew install k6
k6 run --out json=k6/results.json k6/load-test.js
```

See [k6/LOAD_TEST_CASE_STUDY.md](./k6/LOAD_TEST_CASE_STUDY.md) for the full debugging history — three failed runs, what each failure revealed about the system, and what was fixed before the final clean run.

---

## Background

This project was built in 2 weeks as a capstone for learning Go and distributed systems. I am a senior engineer with 10+ years in PHP/Laravel, Vue.js, and React — with **zero prior Go experience** before starting this project.

Most of the implementation was done with the assistance of Claude Code (Anthropic's AI coding tool). The goal was not to write every line manually, but to learn how to work with Go idioms, understand distributed systems design decisions, and build something that would be meaningful to a payments or fintech company interviewer. Every architectural choice — saga pattern, idempotency, optimistic locking, circuit breaking, reconciliation — was studied, questioned, and understood rather than blindly generated.

This is the first large Go codebase I have shipped.

See [ARCHITECTURE.md](./ARCHITECTURE.md) for the full system design and [DEPLOY.md](./DEPLOY.md) for deployment instructions.
