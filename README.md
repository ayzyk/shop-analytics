# shop-analytics

A command line tool that answers business questions about an online shop with SQL:
how fast do we grow, do customers come back, who are the best customers, what is bought together.

Most of this project is the SQL in [`internal/reports/queries`](internal/reports/queries), Go only generates the data, runs the queries and prints the result.
I made it to practice window functions and CTEs on something more interesting than textbook tasks.

## Run

```bash
make db
export DATABASE_URL='postgres://shop:shop@localhost:5435/shop?sslmode=disable'

go run ./cmd/shopstats seed -customers 20000 -orders 200000
# loaded 20000 customers, 200000 orders, 543390 order items in 4.513s

go run ./cmd/shopstats list
go run ./cmd/shopstats report monthly_revenue -from 2025-07-01
go run ./cmd/shopstats report rfm -format csv > rfm.csv
```

The data is generated, the same `-seed` always gives the same data. It is skewed on purpose (some customers buy a lot more, there is a
holiday peak, some products sell in pairs), so do not read the numbers below as real market data.

## Reports

### monthly_revenue

Uses `LAG` for growth, a window frame for the moving average and `PARTITION BY` year for the running total.

```
month       orders  customers  revenue     avg_order  growth_pct  avg_3_months  year_to_date
2025-07-01  8746    4551       6034245.79  689.94                 6034245.79    6034245.79
2025-08-01  9257    4611       6368412.35  687.96     5.5         6201329.07    12402658.14
2025-09-01  9379    4637       6452766.57  688.00     1.3         6285141.57    18855424.71
2025-10-01  10092   4756       6924247.88  686.11     7.3         6581808.93    25779672.59
2025-11-01  13342   5461       9205350.88  689.95     32.9        7527455.11    34985023.47
2025-12-01  10652   4847       7429957.71  697.52     -19.3       7853185.49    42414981.18
```

### cohort_retention

Groups customers by the month of the first order and shows what percent bought again N months later. Pivoted with `COUNT(*) FILTER (WHERE ...)`.

```
cohort      customers  m1    m2    m3    m6    m12
2024-01-01  440        69.5  64.8  53.2  28.6  11.8
2024-02-01  706        69.1  58.8  51.6  30.9  8.8
2024-03-01  841        63.1  61.0  52.9  30.9  8.3
2024-04-01  893        65.8  59.0  50.5  31.2  6.7
2024-05-01  892        63.0  55.4  47.9  39.0  7.6
2024-06-01  879        64.2  54.3  48.0  37.4  8.3
```

### top_products

Top N in each category with `DENSE_RANK`, products with the same revenue share a place.

```
category     rank  product         units  revenue     share_pct
Books        1     Books 01        59624  9125453.20  28.3
Books        2     Books 02        42928  5961840.64  18.5
Electronics  1     Electronics 02  11676  2210967.36  18.8
Electronics  2     Electronics 05  10173  2051487.18  17.5
```

### rfm

Scores customers from 1 to 5 by recency and frequency with `NTILE`, then gives each one a segment.

```
segment          customers  avg_recency_days  avg_orders  avg_spend  revenue      revenue_pct
champions        4057       24                20.2        14022.09   56887600.49  44.9
at_risk          3986       332               9.5         6598.76    26302657.91  20.8
loyal            3870       94                9.7         6712.10    25975828.45  20.5
needs_attention  3236       206               3.4         2223.32    7194647.92   5.7
new              2403       32                3.6         2310.95    5553215.72   4.4
lost             2305       457               3.1         2059.01    4746021.67   3.7
```

`at_risk` is the useful line here: customers who used to buy often and have not been back for almost a year.

### basket_pairs

Finds products from the same order with a self join and sorts them by lift. Sorting by count would only show the bestsellers, which land in one order by chance.

```
product_a  product_b  orders  support_pct  confidence_pct  lift
Garden 09  Garden 10  2482    1.35         57.0            24.50
Garden 07  Garden 08  2562    1.39         57.3            24.05
Garden 03  Garden 04  2652    1.44         58.0            23.69
Garden 05  Garden 06  2653    1.44         58.6            23.64
Sports 09  Sports 10  2708    1.47         57.8            22.83
```

The generator puts products in pairs (1 with 2, 3 with 4...), and the query found exactly those pairs.

## Does the index help?

I expected `orders_paid_created_at_idx` to speed up everything that filters by date, so I measured it with `EXPLAIN ANALYZE` on 200 000 orders:

| Period | With index | Without index |
| --- | --- | --- |
| 1 day (454 orders) | ~2 ms, Bitmap Index Scan | ~8 ms, Parallel Seq Scan |
| 1 month (13 342 orders) | ~43 ms, Bitmap Index Scan | ~21 ms, Parallel Seq Scan |

On one day the index wins. On one month it is about two times **slower**: the planner picks the index, loses the parallel workers,
and a sequential scan of a table this small is very cheap anyway. So the planner's choice was not the best one here, and an index
is not a free speedup. On a much bigger table the result would likely change, I have not tested that yet.

## How the reports are tested

[`reports_test.go`](internal/reports/reports_test.go) loads six orders that are small enough to calculate by hand,
and compares each report with the exact expected rows. The cancelled order in this data is the largest one, so a report
that forgets to filter by status fails right away.

```bash
make test
```

## Decisions

- **Money is stored in cents**, the conversion to a decimal happens in the query with `/ 100.0`, so no floats.
- **`unit_price_cents` is copied into `order_items`**, because changing a price must not change old orders.
- **The `paid_lines` view** keeps the definition of revenue in one place, reports can not disagree about it.
- **The connection is forced to UTC**, because `date_trunc('month', ...)` on a `timestamptz` depends on the session time zone and an order could move to another month.
- **The period in `cohort_retention` selects cohorts only.** The first order is always searched in the full history, otherwise old customers would look new.

## Known limitations

- `NTILE` splits customers into equal groups, so two customers with the same values can get different scores.
  There is a tie-breaker to keep the result stable, but it does not make this fair.
- `basket_pairs` scans all pairs in the period and will get slow on a big catalog, it needs a minimum support filter.
- The reports are computed from raw data every time. Materialized views would be the next step for a dashboard.
