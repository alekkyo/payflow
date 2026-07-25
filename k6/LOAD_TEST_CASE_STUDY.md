# Load Test Case Study — PayFlow

## Goal

Measure the real-world performance of the order creation flow under concurrent load: how fast does the API respond, how many requests per second can the system sustain, and does it stay stable under pressure?

The test scenario mirrors a realistic user journey:

1. Authenticate (once per virtual user)
2. Fetch the product catalogue to get real product IDs
3. Place a new order every 500 ms using a unique `Idempotency-Key`

All tests run against the live production environment (`payflow.alexkua.com`).

---

## Run 1 — First attempt, 50 VUs

**Config:** 50 virtual users, rate limit 5 req/min per user, backpressure threshold 500 messages.

**Results:**

| Metric | Value |
|---|---|
| Successful orders | 5 / 5,141 |
| HTTP error rate | 99.86 % |
| p99 latency | 19.32 s |
| Timeouts | Many (60 s each) |

**What was detected:**

Login and product fetches succeeded, but nearly every order request timed out after 60 seconds. Exactly 5 orders went through — which matched the configured rate limit of 5 requests per minute.

**Root cause: per-user rate limiter.**

The order creation endpoint has a sliding window rate limit of 5 requests per minute per authenticated user. All 50 virtual users shared the same demo customer account. The first 5 requests went through; every subsequent request hit the rate limit and was rejected with 429 — Too Many Requests. The 60-second timeouts were not the rate limiter itself (which responds instantly) but a small number of requests that the overwhelmed server dropped at the connection level.

**Fix:** Temporarily raised the rate limit from `5/min` to `1,000/min` for the duration of the test.

---

## Run 2 — Rate limit raised, 50 VUs

**Config:** 50 virtual users, rate limit 1,000 req/min, backpressure threshold 500 messages.

**Results:**

| Metric | Value |
|---|---|
| Successful orders | 491 / 5,141 |
| HTTP error rate | 89.25 % |
| p99 latency | 19.42 s |
| Timeouts | Still present |

**What was detected:**

491 orders succeeded this time — 98× more than Run 1 — confirming the rate limit was the primary blocker in Run 1. But the error rate was still 89%. The successful requests were very fast (p95 = 52 ms), meaning the system itself was healthy. Something was still blocking the majority of requests.

**Root cause: backpressure middleware.**

PayFlow has a backpressure middleware on the order creation endpoint. It reads the current depth of the `orders.created` Redis Stream. If the queue has more than 500 unprocessed messages, the API returns 503 — Service Unavailable and tells the client to retry in 30 seconds.

With 50 virtual users generating ~40 orders per second, the async workers (inventory, payment, webhook) could not drain the queue fast enough. The queue depth crossed 500 quickly, and every subsequent order was rejected to prevent the backlog from growing unbounded. This is the backpressure mechanism working as designed — but it meant the load test was measuring the rate limiter and backpressure system, not the API's actual throughput.

**Fix:** Reduced virtual users from 50 to 20 (to stay within the workers' processing capacity) and temporarily raised the backpressure threshold from 500 to 5,000 messages.

---

## Run 3 — Backpressure raised, 20 VUs

**Config:** 20 virtual users, rate limit 1,000 req/min, backpressure threshold 5,000 messages.

**Results:**

| Metric | Value |
|---|---|
| HTTP 201 (new order) | 710 |
| HTTP 200 (idempotency hit) | 490 |
| HTTP 429 (rate limited) | 2,206 |
| HTTP error rate | 64.76 % |
| p99 latency | 78 ms ✓ |

**What was detected:**

Two things emerged here:

**1. Rate limit still being hit.** With 20 VUs each placing an order every 500 ms, the total request rate was ~40 req/s = 2,400 req/min. The rate limit was only 1,000/min — still too low. 2,206 requests were rejected with 429.

**2. Idempotency key collisions across test runs.** 490 requests returned HTTP 200 instead of 201. A 201 means a new order was created; a 200 means the idempotency key was already seen and the server returned the existing order instead. This happened because the idempotency keys were generated as `k6-vu{VU}-iter{ITER}` — and `__ITER` resets to 0 at the start of each test run. The keys from Run 1 and Run 2 were still cached in Redis, so VU 1 iter 0 in Run 3 matched VU 1 iter 0 from a previous run.

Notably, **the latency threshold passed for the first time** — p99 was 78 ms, well under the 3 s threshold. This confirmed the system's actual response time was healthy; only the rate limiter was causing failures.

**Fix:** Raised the rate limit to 10,000/min and added a `RUN_ID` timestamp prefix to all idempotency keys (`k6-{timestamp}-vu{VU}-iter{ITER}`), making keys globally unique across test runs.

---

## Run 4 — Clean run, final results

**Config:** 20 virtual users, rate limit 10,000 req/min, backpressure threshold 5,000 messages, unique idempotency keys per run.

**Results:**

| Metric | Value |
|---|---|
| Total orders placed | 3,388 |
| Sustained throughput | 18.75 req/s |
| Order creation p50 | 39 ms |
| Order creation p95 | 52 ms |
| Order creation p99 | 78 ms |
| HTTP error rate | 0 % |
| Checks passed | 6,778 / 6,778 (100 %) |

Both thresholds passed:
- `p(99) < 3,000 ms` ✓ — actual p99: **78 ms**
- `error rate < 5 %` ✓ — actual error rate: **0 %**

**What the numbers mean:**

The system sustained 18.75 orders per second under 20 concurrent users with zero failures. 99% of order creation requests completed in under 78 milliseconds end-to-end, including TLS overhead from a remote client. This covers the full synchronous path: idempotency check in Redis, database transaction (order + items + event), publishing to Redis Streams, and returning the response. Async saga processing (inventory reservation, payment, reconciliation) continues in the background after the API responds.

---

## What was restored after testing

The rate limit and backpressure thresholds were temporarily raised to isolate system performance from production safety mechanisms. Both were reverted to their original values after the test:

| Setting | Test value | Production value | Purpose |
|---|---|---|---|
| Order rate limit | 10,000 req/min | 5 req/min | Prevents a single user from flooding the order queue |
| Backpressure threshold | 5,000 messages | 500 messages | Rejects new orders when workers fall behind, preventing unbounded queue growth |

These limits are intentional. In production, a real customer placing more than 5 orders per minute is either a bug or abuse. The backpressure threshold ensures the system degrades gracefully under worker slowdown rather than accepting orders it cannot fulfil in a reasonable time.

---

## Key learnings

- **Test interference is real.** Three separate production safety mechanisms (rate limiter, backpressure, idempotency cache) each impacted the test results before we isolated the actual system performance. Diagnosing each one required reading the HTTP status codes returned, not just the error rate.
- **503 and 429 are not the same failure.** 429 means the client is sending too fast; 503 means the server's downstream is overwhelmed. Both look like "failures" in a load test but have completely different root causes and fixes.
- **Idempotency keys must be globally unique across test runs.** Reusing the same keys across runs causes the server to return cached results rather than exercising the full code path, producing misleadingly low latency and incorrect status codes.
- **The system's actual throughput ceiling was not reached.** At 20 VUs the system ran at 0 % error rate with headroom. The real capacity limit (where errors begin) was not explored in this test run — that would require a stress test with a much higher VU count and the safety limits intentionally disabled.
