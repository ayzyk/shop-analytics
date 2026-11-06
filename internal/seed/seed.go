// Package seed creates the schema and fills it with generated shop data.
package seed

import (
	"context"
	_ "embed"
	"fmt"
	"math"
	"math/rand/v2"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed schema.sql
var schema string

type Config struct {
	Customers int
	Orders    int
	Seed      uint64
}

type Customer struct {
	ID        int64
	Email     string
	City      string
	CreatedAt time.Time
}

type Product struct {
	ID         int64
	CategoryID int64
	Name       string
	PriceCents int64
}

type Order struct {
	ID         int64
	CustomerID int64
	Status     string
	CreatedAt  time.Time
}

type OrderItem struct {
	OrderID        int64
	ProductID      int64
	Quantity       int32
	UnitPriceCents int64
}

type Data struct {
	Customers []Customer
	Products  []Product
	Orders    []Order
	Items     []OrderItem
}

var categories = []string{"Books", "Electronics", "Games", "Kitchen", "Sports", "Garden"}
var cities = []string{"Moscow", "Saint Petersburg", "Kazan", "Novosibirsk", "Yekaterinburg", "Samara"}

const productsPerCategory = 10

var (
	periodStart = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	periodEnd   = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
)

// Generate is deterministic: the same config always gives the same data.
//
// Uniform random data makes boring reports, so a few things are built in:
// some customers buy much more than others, activity fades after signup,
// November and December are busier, and some products are often bought together.
func Generate(cfg Config) Data {
	rng := rand.New(rand.NewPCG(cfg.Seed, 1))
	period := periodEnd.Sub(periodStart)

	var data Data
	for i := 1; i <= cfg.Customers; i++ {
		data.Customers = append(data.Customers, Customer{
			ID:    int64(i),
			Email: fmt.Sprintf("customer%d@example.com", i),
			City:  cities[rng.IntN(len(cities))],
			// the last months are left out, otherwise new customers have no time to buy
			CreatedAt: periodStart.Add(time.Duration(rng.Float64() * 0.9 * float64(period))),
		})
	}

	for c := range categories {
		for p := 1; p <= productsPerCategory; p++ {
			data.Products = append(data.Products, Product{
				ID:         int64(len(data.Products) + 1),
				CategoryID: int64(c + 1),
				Name:       fmt.Sprintf("%s %02d", categories[c], p),
				PriceCents: int64(500 + rng.IntN(20000)),
			})
		}
	}

	for i := 1; i <= cfg.Orders; i++ {
		customer := data.Customers[pickCustomer(rng, cfg.Customers)]
		order := Order{
			ID:         int64(i),
			CustomerID: customer.ID,
			Status:     pickStatus(rng),
			CreatedAt:  pickOrderTime(rng, customer.CreatedAt),
		}
		data.Orders = append(data.Orders, order)
		data.Items = append(data.Items, pickItems(rng, order.ID, data.Products)...)
	}
	return data
}

// pickCustomer is skewed: customers with low indexes get most of the orders.
func pickCustomer(rng *rand.Rand, n int) int {
	return int(math.Pow(rng.Float64(), 2.5) * float64(n))
}

func pickStatus(rng *rand.Rand) string {
	switch r := rng.Float64(); {
	case r < 0.92:
		return "paid"
	case r < 0.97:
		return "cancelled"
	default:
		return "refunded"
	}
}

func pickOrderTime(rng *rand.Rand, signup time.Time) time.Time {
	const meanDaysAfterSignup = 120
	for {
		days := rng.ExpFloat64() * meanDaysAfterSignup
		t := signup.Add(time.Duration(days * float64(24*time.Hour)))
		if t.After(periodEnd) {
			continue
		}
		// keep only a part of the orders outside of the holiday season
		if m := t.Month(); m != time.November && m != time.December && rng.Float64() > 0.6 {
			continue
		}
		return t
	}
}

func pickItems(rng *rand.Rand, orderID int64, products []Product) []OrderItem {
	seen := map[int]bool{}
	var items []OrderItem
	add := func(index int) {
		if seen[index] {
			return
		}
		seen[index] = true
		items = append(items, OrderItem{
			OrderID:        orderID,
			ProductID:      products[index].ID,
			Quantity:       int32(1 + rng.IntN(3)),
			UnitPriceCents: products[index].PriceCents,
		})
	}

	for n := 1 + rng.IntN(3); n > 0; n-- {
		// squared, so that the first products in the catalog sell better
		index := int(math.Pow(rng.Float64(), 2) * float64(len(products)))
		add(index)
		// products in pairs (0,1), (2,3)... are often bought together
		if rng.Float64() < 0.4 {
			add(index ^ 1)
		}
	}
	return items
}

func ApplySchema(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, schema); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	return nil
}

// Load replaces everything in the database with the given data.
func Load(ctx context.Context, pool *pgxpool.Pool, data Data) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `TRUNCATE order_items, orders, products, categories, customers`)
	if err != nil {
		return fmt.Errorf("truncate: %w", err)
	}

	for i, name := range categories {
		_, err := tx.Exec(ctx, `INSERT INTO categories (id, name) VALUES ($1, $2)`, i+1, name)
		if err != nil {
			return fmt.Errorf("insert category: %w", err)
		}
	}

	// COPY is much faster than inserting row by row
	copies := []struct {
		table   string
		columns []string
		rows    pgx.CopyFromSource
	}{
		{"customers", []string{"id", "email", "city", "created_at"},
			rowsOf(data.Customers, func(c Customer) []any { return []any{c.ID, c.Email, c.City, c.CreatedAt} })},
		{"products", []string{"id", "category_id", "name", "price_cents"},
			rowsOf(data.Products, func(p Product) []any { return []any{p.ID, p.CategoryID, p.Name, p.PriceCents} })},
		{"orders", []string{"id", "customer_id", "status", "created_at"},
			rowsOf(data.Orders, func(o Order) []any { return []any{o.ID, o.CustomerID, o.Status, o.CreatedAt} })},
		{"order_items", []string{"order_id", "product_id", "quantity", "unit_price_cents"},
			rowsOf(data.Items, func(i OrderItem) []any { return []any{i.OrderID, i.ProductID, i.Quantity, i.UnitPriceCents} })},
	}
	for _, c := range copies {
		if _, err := tx.CopyFrom(ctx, pgx.Identifier{c.table}, c.columns, c.rows); err != nil {
			return fmt.Errorf("copy %s: %w", c.table, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	// the planner needs fresh statistics after a bulk load
	if _, err := pool.Exec(ctx, `ANALYZE`); err != nil {
		return fmt.Errorf("analyze: %w", err)
	}
	return nil
}

func rowsOf[T any](items []T, toRow func(T) []any) pgx.CopyFromSource {
	return pgx.CopyFromSlice(len(items), func(i int) ([]any, error) {
		return toRow(items[i]), nil
	})
}
