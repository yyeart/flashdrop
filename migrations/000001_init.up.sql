CREATE SCHEMA flashdrop;

CREATE TABLE flashdrop.users (
    id         UUID                 PRIMARY KEY,
    role       TEXT        NOT NULL CHECK (role IN ('user', 'admin')),
    created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE flashdrop.sales (
    id         UUID        PRIMARY KEY,
    state      TEXT        NOT NULL     CHECK (state IN ('draft', 'active', 'ended')),
    starts_at  TIMESTAMPTZ NOT NULL,
    ends_at    TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,

    CHECK (starts_at < ends_at)
);

CREATE TABLE flashdrop.sale_items (
    id           UUID             PRIMARY KEY,
    sale_id      UUID    NOT NULL REFERENCES flashdrop.sales(id),
    product_id   UUID    NOT NULL,
    name         TEXT    NOT NULL,
    price_minor  BIGINT  NOT NULL,
    total_qty    INTEGER NOT NULL,
    reserved_qty INTEGER NOT NULL,
    sold_qty     INTEGER NOT NULL,

    CHECK (
        (price_minor > 0 AND total_qty > 0 AND reserved_qty >= 0 AND sold_qty >= 0) 
        AND 
        (reserved_qty <= total_qty AND sold_qty <= total_qty - reserved_qty)
    )
);

CREATE INDEX idx_sale_id ON flashdrop.sale_items(sale_id);

CREATE TABLE flashdrop.reservations (
    id           UUID                 PRIMARY KEY,
    user_id      UUID        NOT NULL REFERENCES flashdrop.users(id),
    sale_item_id UUID        NOT NULL REFERENCES flashdrop.sale_items(id),
    quantity     INTEGER     NOT NULL CHECK (quantity > 0),
    state        TEXT        NOT NULL CHECK (state in ('pending', 'paid', 'cancelled', 'expired')),
    created_at   TIMESTAMPTZ NOT NULL,
    expires_at   TIMESTAMPTZ NOT NULL,

    CHECK (created_at < expires_at)
);

CREATE INDEX idx_reservations_pending_expires_at ON flashdrop.reservations(expires_at) 
    WHERE state = 'pending';

CREATE TABLE flashdrop.orders (
    id             UUID                 PRIMARY KEY,
    reservation_id UUID        NOT NULL REFERENCES flashdrop.reservations(id) UNIQUE,
    user_id        UUID        NOT NULL REFERENCES flashdrop.users(id),
    sale_item_id   UUID        NOT NULL REFERENCES flashdrop.sale_items(id),
    quantity       INTEGER     NOT NULL CHECK (quantity > 0),
    created_at     TIMESTAMPTZ NOT NULL
);

CREATE TABLE flashdrop.idempotency_records (
    id              UUID                 PRIMARY KEY,
    user_id         UUID        NOT NULL REFERENCES flashdrop.users(id),
    idempotency_key TEXT        NOT NULL,
    request_hash    CHAR(64)    NOT NULL,
    response_result JSONB       NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL,

    CONSTRAINT uk_user_idempotency UNIQUE (user_id, idempotency_key)
);
