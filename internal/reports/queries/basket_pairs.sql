-- "Customers who bought this also bought": pairs of products from the same order.
--
-- lift > 1 means the pair appears more often than if the products were independent.
-- Plain counts are not enough, two bestsellers are often in one order just by chance.
WITH paid AS (
    SELECT DISTINCT order_id, product_id
    FROM paid_lines
    WHERE ordered_at >= @from::timestamptz
      AND ordered_at <  @to::timestamptz
),
total AS (
    SELECT COUNT(DISTINCT order_id)::numeric AS orders FROM paid
),
per_product AS (
    SELECT product_id, COUNT(*)::numeric AS orders
    FROM paid
    GROUP BY product_id
),
pairs AS (
    SELECT a.product_id AS product_a,
           b.product_id AS product_b,
           COUNT(*)::numeric AS orders
    FROM paid a
    JOIN paid b ON a.order_id = b.order_id
               AND a.product_id < b.product_id -- every pair once, and not a product with itself
    GROUP BY a.product_id, b.product_id
)
SELECT pa.name AS product_a,
       pb.name AS product_b,
       p.orders::bigint AS orders,
       ROUND(100 * p.orders / t.orders, 2) AS support_pct,
       ROUND(100 * p.orders / a.orders, 1) AS confidence_pct,
       ROUND((p.orders / t.orders) / ((a.orders / t.orders) * (b.orders / t.orders)), 2) AS lift
FROM pairs p
CROSS JOIN total t
JOIN per_product a ON a.product_id = p.product_a
JOIN per_product b ON b.product_id = p.product_b
JOIN products pa ON pa.id = p.product_a
JOIN products pb ON pb.id = p.product_b
ORDER BY lift DESC, orders DESC, product_a, product_b
LIMIT @limit;
