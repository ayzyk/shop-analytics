-- Customers are grouped by the month of their first paid order.
-- m1 is the percent of a cohort that bought again one month later, and so on.
--
-- The first order is searched in all history, the period only selects the cohorts.
-- Otherwise an old customer would look like a new one.
WITH first_order AS (
    SELECT customer_id,
           date_trunc('month', MIN(ordered_at))::date AS cohort
    FROM paid_lines
    GROUP BY customer_id
),
activity AS (
    SELECT DISTINCT
           customer_id,
           date_trunc('month', ordered_at)::date AS month
    FROM paid_lines
),
offsets AS (
    SELECT f.cohort,
           a.customer_id,
           (EXTRACT(YEAR FROM a.month) - EXTRACT(YEAR FROM f.cohort)) * 12
               + EXTRACT(MONTH FROM a.month) - EXTRACT(MONTH FROM f.cohort) AS month_number
    FROM first_order f
    JOIN activity a USING (customer_id)
    WHERE f.cohort >= @from::timestamptz
      AND f.cohort <  @to::timestamptz
)
SELECT cohort,
       COUNT(DISTINCT customer_id) AS customers,
       ROUND(100.0 * COUNT(*) FILTER (WHERE month_number = 1) / COUNT(DISTINCT customer_id), 1) AS m1,
       ROUND(100.0 * COUNT(*) FILTER (WHERE month_number = 2) / COUNT(DISTINCT customer_id), 1) AS m2,
       ROUND(100.0 * COUNT(*) FILTER (WHERE month_number = 3) / COUNT(DISTINCT customer_id), 1) AS m3,
       ROUND(100.0 * COUNT(*) FILTER (WHERE month_number = 6) / COUNT(DISTINCT customer_id), 1) AS m6,
       ROUND(100.0 * COUNT(*) FILTER (WHERE month_number = 12) / COUNT(DISTINCT customer_id), 1) AS m12
FROM offsets
GROUP BY cohort
ORDER BY cohort;
