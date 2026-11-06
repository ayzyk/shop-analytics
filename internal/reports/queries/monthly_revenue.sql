-- Revenue per month with growth, a moving average and a running total for the year.
WITH monthly AS (
    SELECT date_trunc('month', ordered_at)::date AS month,
           COUNT(DISTINCT order_id)              AS orders,
           COUNT(DISTINCT customer_id)           AS customers,
           SUM(line_cents) / 100.0               AS revenue
    FROM paid_lines
    WHERE ordered_at >= @from::timestamptz
      AND ordered_at <  @to::timestamptz
    GROUP BY 1
)
SELECT month,
       orders,
       customers,
       ROUND(revenue, 2)          AS revenue,
       ROUND(revenue / orders, 2) AS avg_order,
       ROUND(100 * (revenue - LAG(revenue) OVER by_month)
             / NULLIF(LAG(revenue) OVER by_month, 0), 1) AS growth_pct,
       ROUND(AVG(revenue) OVER (ORDER BY month ROWS BETWEEN 2 PRECEDING AND CURRENT ROW), 2) AS avg_3_months,
       ROUND(SUM(revenue) OVER (PARTITION BY date_trunc('year', month) ORDER BY month), 2)   AS year_to_date
FROM monthly
WINDOW by_month AS (ORDER BY month)
ORDER BY month;
