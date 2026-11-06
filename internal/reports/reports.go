// Package reports runs the SQL files from the queries folder.
//
// The SQL lives in separate files on purpose: a query can be opened and
// tried in psql or a GUI client, and adding a report does not need Go code.
package reports

import (
	"context"
	"embed"
	"encoding/csv"
	"fmt"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed queries/*.sql
var queries embed.FS

type Params struct {
	From  time.Time
	To    time.Time
	Limit int
}

type Table struct {
	Columns []string
	Rows    [][]string
}

// Names returns all available reports.
func Names() []string {
	files, err := fs.Glob(queries, "queries/*.sql")
	if err != nil {
		panic(err) // the pattern is a constant, so this can only be a programmer error
	}
	names := make([]string, 0, len(files))
	for _, f := range files {
		names = append(names, strings.TrimSuffix(path.Base(f), ".sql"))
	}
	sort.Strings(names)
	return names
}

func Run(ctx context.Context, pool *pgxpool.Pool, name string, p Params) (*Table, error) {
	sql, err := queries.ReadFile("queries/" + name + ".sql")
	if err != nil {
		return nil, fmt.Errorf("unknown report %q, available: %s", name, strings.Join(Names(), ", "))
	}

	// Text format: the values only get printed, so there is no need
	// to convert every column type to a Go type and back to a string.
	rows, err := pool.Query(ctx, string(sql),
		pgx.QueryResultFormats{pgx.TextFormatCode},
		pgx.NamedArgs{"from": p.From, "to": p.To, "limit": p.Limit},
	)
	if err != nil {
		return nil, fmt.Errorf("report %s: %w", name, err)
	}
	defer rows.Close()

	table := &Table{}
	for _, field := range rows.FieldDescriptions() {
		table.Columns = append(table.Columns, field.Name)
	}
	for rows.Next() {
		row := make([]string, 0, len(table.Columns))
		for _, value := range rows.RawValues() {
			row = append(row, string(value)) // NULL becomes an empty string
		}
		table.Rows = append(table.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("report %s: %w", name, err)
	}
	return table, nil
}

func (t *Table) WriteText(w io.Writer) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(t.Columns, "\t"))
	for _, row := range t.Rows {
		fmt.Fprintln(tw, strings.Join(row, "\t"))
	}
	return tw.Flush()
}

func (t *Table) WriteCSV(w io.Writer) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(t.Columns); err != nil {
		return err
	}
	if err := cw.WriteAll(t.Rows); err != nil {
		return err
	}
	return cw.Error()
}
