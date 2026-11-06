package reports

import (
	"bytes"
	"context"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ayzyk/shop-analytics/internal/seed"
)

// Small enough to calculate the expected numbers by hand:
//
//	order  customer  date        status     items
//	1      1         2024-01-10  paid       Go Book x1 (100), SQL Book x2 (100)   = 200
//	2      2         2024-01-20  paid       Go Book x1 (100), SQL Book x1 (50)    = 150
//	3      1         2024-02-05  paid       Chess x1 (200)                        = 200
//	4      3         2024-02-15  cancelled  Chess x5 (1000), must never be counted
//	5      3         2024-03-01  paid       SQL Book x2 (100)                     = 100
//	6      1         2024-03-20  paid       Go Book x1 (100), SQL Book x1 (50)    = 150
const fixture = `
TRUNCATE order_items, orders, products, categories, customers;
INSERT INTO categories (id, name) VALUES (1, 'Books'), (2, 'Games');
INSERT INTO products (id, category_id, name, price_cents) VALUES
    (1, 1, 'Go Book', 10000), (2, 1, 'SQL Book', 5000), (3, 2, 'Chess', 20000);
INSERT INTO customers (id, email, city, created_at) VALUES
    (1, 'a@example.com', 'Kazan', '2024-01-01Z'),
    (2, 'b@example.com', 'Kazan', '2024-01-01Z'),
    (3, 'c@example.com', 'Kazan', '2024-01-01Z');
INSERT INTO orders (id, customer_id, status, created_at) VALUES
    (1, 1, 'paid',      '2024-01-10 12:00Z'),
    (2, 2, 'paid',      '2024-01-20 12:00Z'),
    (3, 1, 'paid',      '2024-02-05 12:00Z'),
    (4, 3, 'cancelled', '2024-02-15 12:00Z'),
    (5, 3, 'paid',      '2024-03-01 12:00Z'),
    (6, 1, 'paid',      '2024-03-20 12:00Z');
INSERT INTO order_items (order_id, product_id, quantity, unit_price_cents) VALUES
    (1, 1, 1, 10000), (1, 2, 2, 5000),
    (2, 1, 1, 10000), (2, 2, 1, 5000),
    (3, 3, 1, 20000),
    (4, 3, 5, 20000),
    (5, 2, 2, 5000),
    (6, 1, 1, 10000), (6, 2, 1, 5000);
`

func setup(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()

	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["timezone"] = "UTC"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	if err := seed.ApplySchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, fixture); err != nil {
		t.Fatal(err)
	}
	return pool
}

func period() Params {
	return Params{
		From:  time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		To:    time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC),
		Limit: 10,
	}
}

func assertRows(t *testing.T, got *Table, want [][]string) {
	t.Helper()
	if !reflect.DeepEqual(got.Rows, want) {
		t.Errorf("columns: %v\n got: %v\nwant: %v", got.Columns, got.Rows, want)
	}
}

func TestReports(t *testing.T) {
	pool := setup(t)

	tests := []struct {
		report string
		want   [][]string
	}{
		{
			report: "monthly_revenue",
			// month, orders, customers, revenue, avg_order, growth_pct, avg_3_months, year_to_date
			want: [][]string{
				{"2024-01-01", "2", "2", "350.00", "175.00", "", "350.00", "350.00"},
				{"2024-02-01", "1", "1", "200.00", "200.00", "-42.9", "275.00", "550.00"},
				{"2024-03-01", "2", "2", "250.00", "125.00", "25.0", "266.67", "800.00"},
			},
		},
		{
			report: "cohort_retention",
			// customers 1 and 2 start in January, only customer 1 comes back in Feb and Mar
			want: [][]string{
				{"2024-01-01", "2", "50.0", "50.0", "0.0", "0.0", "0.0"},
				{"2024-03-01", "1", "0.0", "0.0", "0.0", "0.0", "0.0"},
			},
		},
		{
			report: "top_products",
			// both books have 300 of revenue, so they share rank 1
			want: [][]string{
				{"Books", "1", "Go Book", "3", "300.00", "50.0"},
				{"Books", "1", "SQL Book", "6", "300.00", "50.0"},
				{"Games", "1", "Chess", "1", "200.00", "100.0"},
			},
		},
		{
			report: "rfm",
			// customer 1 is loyal, 2 has not bought for 71 days, 3 only has one small order
			want: [][]string{
				{"loyal", "1", "11", "3.0", "550.00", "550.00", "68.8"},
				{"lost", "1", "71", "1.0", "150.00", "150.00", "18.8"},
				{"needs_attention", "1", "30", "1.0", "100.00", "100.00", "12.5"},
			},
		},
		{
			report: "basket_pairs",
			// 3 of 5 orders have both. Go Book is in 3 orders, SQL Book in 4.
			// lift = 0.6 / (0.6 * 0.8) = 1.25
			want: [][]string{
				{"Go Book", "SQL Book", "3", "60.00", "100.0", "1.25"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.report, func(t *testing.T) {
			got, err := Run(context.Background(), pool, tt.report, period())
			if err != nil {
				t.Fatal(err)
			}
			assertRows(t, got, tt.want)
		})
	}
}

func TestTopProductsLimit(t *testing.T) {
	pool := setup(t)
	if _, err := pool.Exec(context.Background(),
		`UPDATE order_items SET quantity = 1 WHERE order_id = 1 AND product_id = 2`); err != nil {
		t.Fatal(err)
	}
	p := period()
	p.Limit = 1
	got, err := Run(context.Background(), pool, "top_products", p)
	if err != nil {
		t.Fatal(err)
	}
	// SQL Book has less revenue now and does not get into the top 1
	assertRows(t, got, [][]string{
		{"Books", "1", "Go Book", "3", "300.00", "54.5"},
		{"Games", "1", "Chess", "1", "200.00", "100.0"},
	})
}

func TestPeriodFilter(t *testing.T) {
	pool := setup(t)
	p := period()
	p.From = time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)
	p.To = time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)

	got, err := Run(context.Background(), pool, "monthly_revenue", p)
	if err != nil {
		t.Fatal(err)
	}
	assertRows(t, got, [][]string{
		{"2024-02-01", "1", "1", "200.00", "200.00", "", "200.00", "200.00"},
	})
}

func TestUnknownReport(t *testing.T) {
	_, err := Run(context.Background(), nil, "nope", period())
	if err == nil || !strings.Contains(err.Error(), "monthly_revenue") {
		t.Errorf("err = %v, it should list the available reports", err)
	}
}

func TestNames(t *testing.T) {
	want := []string{"basket_pairs", "cohort_retention", "monthly_revenue", "rfm", "top_products"}
	if got := Names(); !reflect.DeepEqual(got, want) {
		t.Errorf("Names() = %v", got)
	}
}

func TestWriteCSV(t *testing.T) {
	table := &Table{Columns: []string{"a", "b"}, Rows: [][]string{{"1", "x,y"}}}
	var buf bytes.Buffer
	if err := table.WriteCSV(&buf); err != nil {
		t.Fatal(err)
	}
	if want := "a,b\n1,\"x,y\"\n"; buf.String() != want {
		t.Errorf("got %q, want %q", buf.String(), want)
	}
}
