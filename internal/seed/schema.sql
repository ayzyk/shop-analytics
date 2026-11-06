CREATE TABLE IF NOT EXISTS customers (
    id         BIGINT PRIMARY KEY,
    email      TEXT        NOT NULL UNIQUE,
    city       TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS categories (
    id   BIGINT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE
);

CREATE TABLE IF NOT EXISTS products (
    id          BIGINT PRIMARY KEY,
    category_id BIGINT NOT NULL REFERENCES categories (id),
    name        TEXT   NOT NULL,
    price_cents BIGINT NOT NULL CHECK (price_cents > 0)
);

CREATE TABLE IF NOT EXISTS orders (
    id          BIGINT PRIMARY KEY,
    customer_id BIGINT      NOT NULL REFERENCES customers (id),
    status      TEXT        NOT NULL CHECK (status IN ('paid', 'cancelled', 'refunded')),
    created_at  TIMESTAMPTZ NOT NULL
);

-- unit_price_cents is a copy: a later price change must not change old orders
CREATE TABLE IF NOT EXISTS order_items (
    order_id         BIGINT NOT NULL REFERENCES orders (id),
    product_id       BIGINT NOT NULL REFERENCES products (id),
    quantity         INT    NOT NULL CHECK (quantity > 0),
    unit_price_cents BIGINT NOT NULL CHECK (unit_price_cents > 0),
    PRIMARY KEY (order_id, product_id)
);

-- almost every report filters paid orders by date
CREATE INDEX IF NOT EXISTS orders_paid_created_at_idx ON orders (created_at) WHERE status = 'paid';
CREATE INDEX IF NOT EXISTS orders_customer_created_at_idx ON orders (customer_id, created_at);
CREATE INDEX IF NOT EXISTS order_items_product_idx ON order_items (product_id);

-- one place that defines what counts as revenue
CREATE OR REPLACE VIEW paid_lines AS
SELECT o.id AS order_id,
       o.customer_id,
       o.created_at AS ordered_at,
       i.product_id,
       i.quantity,
       i.quantity * i.unit_price_cents AS line_cents
FROM orders o
JOIN order_items i ON i.order_id = o.id
WHERE o.status = 'paid';
