package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ayzyk/shop-analytics/internal/reports"
	"github.com/ayzyk/shop-analytics/internal/seed"
)

const usage = `usage:
  shopstats seed   [-customers N] [-orders N] [-seed N]
  shopstats report <name> [-from YYYY-MM-DD] [-to YYYY-MM-DD] [-limit N] [-format text|csv]
  shopstats list

DATABASE_URL has to be set for seed and report.
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		fmt.Print(usage)
		return errors.New("no command")
	}
	switch args[0] {
	case "list":
		fmt.Println(strings.Join(reports.Names(), "\n"))
		return nil
	case "seed":
		return runSeed(ctx, args[1:])
	case "report":
		return runReport(ctx, args[1:])
	default:
		fmt.Print(usage)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func connect(ctx context.Context) (*pgxpool.Pool, error) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return nil, errors.New("DATABASE_URL is not set")
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	// month boundaries depend on the time zone, reports should not depend on the server settings
	cfg.ConnConfig.RuntimeParams["timezone"] = "UTC"
	return pgxpool.NewWithConfig(ctx, cfg)
}

func runSeed(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("seed", flag.ExitOnError)
	customers := fs.Int("customers", 5000, "number of customers")
	orders := fs.Int("orders", 50000, "number of orders")
	seedValue := fs.Uint64("seed", 42, "random seed, the same seed gives the same data")
	fs.Parse(args)

	if *customers < 1 || *orders < 0 {
		return errors.New("customers must be positive and orders must not be negative")
	}

	pool, err := connect(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := seed.ApplySchema(ctx, pool); err != nil {
		return err
	}

	start := time.Now()
	data := seed.Generate(seed.Config{Customers: *customers, Orders: *orders, Seed: *seedValue})
	if err := seed.Load(ctx, pool, data); err != nil {
		return err
	}
	fmt.Printf("loaded %d customers, %d orders, %d order items in %s\n",
		len(data.Customers), len(data.Orders), len(data.Items), time.Since(start).Round(time.Millisecond))
	return nil
}

func runReport(ctx context.Context, args []string) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("report name is required, available: %s", strings.Join(reports.Names(), ", "))
	}
	name := args[0]

	fs := flag.NewFlagSet("report", flag.ExitOnError)
	from := fs.String("from", "2024-01-01", "start of the period, inclusive")
	to := fs.String("to", "2026-01-01", "end of the period, exclusive")
	limit := fs.Int("limit", 10, "max rows for reports with a limit")
	format := fs.String("format", "text", "text or csv")
	fs.Parse(args[1:])

	params := reports.Params{Limit: *limit}
	var err error
	if params.From, err = time.Parse(time.DateOnly, *from); err != nil {
		return fmt.Errorf("bad -from: %w", err)
	}
	if params.To, err = time.Parse(time.DateOnly, *to); err != nil {
		return fmt.Errorf("bad -to: %w", err)
	}
	if !params.From.Before(params.To) {
		return errors.New("-from must be before -to")
	}
	if *format != "text" && *format != "csv" {
		return fmt.Errorf("unknown format %q", *format)
	}

	pool, err := connect(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()

	table, err := reports.Run(ctx, pool, name, params)
	if err != nil {
		return err
	}
	if *format == "csv" {
		return table.WriteCSV(os.Stdout)
	}
	return table.WriteText(os.Stdout)
}
