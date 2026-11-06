-- RFM segmentation: how recently, how often and how much customers buy.
-- Each customer gets a score from 1 to 5 relative to the other customers.
WITH customer_stats AS (
    SELECT customer_id,
           EXTRACT(DAY FROM @to::timestamptz - MAX(ordered_at))::int AS recency_days,
           COUNT(DISTINCT order_id)                                  AS frequency,
           SUM(line_cents) / 100.0                                   AS monetary
    FROM paid_lines
    WHERE ordered_at >= @from::timestamptz
      AND ordered_at <  @to::timestamptz
    GROUP BY customer_id
),
scored AS (
    SELECT *,
           NTILE(5) OVER (ORDER BY recency_days DESC, customer_id) AS r,
           -- most customers have the same frequency, monetary breaks the tie
           NTILE(5) OVER (ORDER BY frequency, monetary, customer_id) AS f
    FROM customer_stats
),
segmented AS (
    SELECT *,
           CASE
               WHEN r >= 4 AND f >= 4 THEN 'champions'
               WHEN r >= 4 AND f <= 2 THEN 'new'
               WHEN r >= 3 AND f >= 3 THEN 'loyal'
               WHEN r <= 2 AND f >= 3 THEN 'at_risk'
               WHEN r <= 1            THEN 'lost'
               ELSE 'needs_attention'
           END AS segment
    FROM scored
)
SELECT segment,
       COUNT(*)                   AS customers,
       ROUND(AVG(recency_days))   AS avg_recency_days,
       ROUND(AVG(frequency), 1)   AS avg_orders,
       ROUND(AVG(monetary), 2)    AS avg_spend,
       ROUND(SUM(monetary), 2)    AS revenue,
       ROUND(100 * SUM(monetary) / SUM(SUM(monetary)) OVER (), 1) AS revenue_pct
FROM segmented
GROUP BY segment
ORDER BY revenue DESC;
