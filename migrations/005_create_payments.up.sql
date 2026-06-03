CREATE TABLE payments (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id          UUID NOT NULL REFERENCES orders(id),
    stripe_payment_id TEXT UNIQUE,
    amount_cents      INT NOT NULL,
    currency          TEXT NOT NULL DEFAULT 'usd',
    status            TEXT NOT NULL,
    idempotency_key   TEXT NOT NULL UNIQUE,
    failure_reason    TEXT,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Append-only payment audit log
CREATE TABLE payment_events (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    payment_id  UUID NOT NULL REFERENCES payments(id),
    event_type  TEXT NOT NULL,
    provider    TEXT NOT NULL DEFAULT 'stripe',
    raw_payload JSONB,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Stripe sends webhooks at least once; deduplicate by event ID
CREATE TABLE processed_webhook_events (
    event_id     TEXT PRIMARY KEY,
    event_type   TEXT NOT NULL,
    processed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE refunds (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    payment_id       UUID NOT NULL REFERENCES payments(id),
    amount_cents     INT NOT NULL,
    reason           TEXT,
    stripe_refund_id TEXT UNIQUE,
    status           TEXT NOT NULL,
    idempotency_key  TEXT NOT NULL UNIQUE,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_payments_order_id ON payments(order_id);
CREATE INDEX idx_payment_events_payment_id ON payment_events(payment_id);
CREATE INDEX idx_refunds_payment_id ON refunds(payment_id);
