CREATE TABLE reconciliation_runs (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_date       DATE NOT NULL UNIQUE,
    status         TEXT NOT NULL,
    matched        INT NOT NULL DEFAULT 0,
    mismatched     INT NOT NULL DEFAULT 0,
    missing_local  INT NOT NULL DEFAULT 0,
    missing_stripe INT NOT NULL DEFAULT 0,
    started_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at   TIMESTAMPTZ
);

CREATE TABLE reconciliation_discrepancies (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    reconciliation_id UUID NOT NULL REFERENCES reconciliation_runs(id),
    payment_id        UUID REFERENCES payments(id),
    stripe_payment_id TEXT,
    discrepancy_type  TEXT NOT NULL,
    our_amount_cents  INT,
    stripe_amount_cents INT,
    our_status        TEXT,
    stripe_status     TEXT,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_reconciliation_discrepancies_run_id ON reconciliation_discrepancies(reconciliation_id);
