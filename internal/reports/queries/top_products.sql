-- The best products inside each category and their share of the category revenue.
WITH sales AS (
    SELECT p.category_id,
           p.id AS product_id,
           p.name,
           SUM(l.quantity)        AS units,
           SUM(l.line_cents) / 100.0 AS revenue
    FROM paid_lines l
    JOIN products p ON p.id = l.product_id
    WHERE l.ordered_at >= @from::timestamptz
      AND l.ordered_at <  @to::timestamptz
    GROUP BY p.category_id, p.id, p.name
),
ranked AS (
    SELECT s.*,
           DENSE_RANK() OVER (PARTITION BY category_id ORDER BY revenue DESC) AS rank,
           100 * revenue / SUM(revenue) OVER (PARTITION BY category_id)       AS share
    FROM sales s
)
SELECT c.name AS category,
       r.rank,
       r.name AS product,
       r.units,
       ROUND(r.revenue, 2) AS revenue,
       ROUND(r.share, 1)   AS share_pct
FROM ranked r
JOIN categories c ON c.id = r.category_id
WHERE r.rank <= @limit
ORDER BY c.name, r.rank, r.name;
