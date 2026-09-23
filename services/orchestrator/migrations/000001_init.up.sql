CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE TABLE orders (
    id UUID PRIMARY KEY,
    client_idempotency_key TEXT NOT NULL UNIQUE,
    request_hash TEXT NOT NULL,
    customer_id TEXT NOT NULL,
    currency TEXT NOT NULL,
    total_cents BIGINT NOT NULL,
    status TEXT NOT NULL,
    cancellation_reason TEXT,
    lease_owner TEXT,
    lease_expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE order_items (
    order_id UUID NOT NULL REFERENCES orders(id),
    sku TEXT NOT NULL,
    quantity INT NOT NULL,
    unit_price_cents BIGINT NOT NULL,
    PRIMARY KEY (order_id, sku)
);

CREATE TABLE saga_steps (
    id SERIAL PRIMARY KEY,
    order_id UUID NOT NULL REFERENCES orders(id),
    step TEXT NOT NULL,
    direction TEXT NOT NULL,
    status TEXT NOT NULL,
    attempts INT NOT NULL DEFAULT 0,
    idempotency_key TEXT NOT NULL,
    last_error TEXT,
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at TIMESTAMPTZ
);
CREATE UNIQUE INDEX saga_steps_order_step_direction_idx ON saga_steps (order_id, step, direction);

CREATE TABLE inventory (
    sku TEXT PRIMARY KEY,
    available INT NOT NULL DEFAULT 0,
    reserved INT NOT NULL DEFAULT 0
);

CREATE TABLE reservations (
    order_id UUID NOT NULL REFERENCES orders(id),
    sku TEXT NOT NULL REFERENCES inventory(sku),
    quantity INT NOT NULL,
    status TEXT NOT NULL,
    PRIMARY KEY (order_id, sku)
);

CREATE TABLE outbox (
    id UUID PRIMARY KEY,
    aggregate_id TEXT NOT NULL,
    type TEXT NOT NULL,
    payload BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    published_at TIMESTAMPTZ
);

-- Insert some test inventory based on the SPEC.md fixtures
INSERT INTO inventory (sku, available, reserved) VALUES
    ('SKU-KEYBOARD', 100, 0),
    ('SKU-MOUSE', 100, 0),
    ('SKU-SOLDOUT', 0, 0),
    ('SKU-1', 100, 0);
