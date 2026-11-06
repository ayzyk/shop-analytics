package seed

import (
	"reflect"
	"testing"
)

func TestGenerateIsDeterministic(t *testing.T) {
	cfg := Config{Customers: 50, Orders: 200, Seed: 7}
	if !reflect.DeepEqual(Generate(cfg), Generate(cfg)) {
		t.Error("same config gave different data")
	}
	other := cfg
	other.Seed = 8
	if reflect.DeepEqual(Generate(cfg), Generate(other)) {
		t.Error("different seeds gave the same data")
	}
}

func TestGenerateIsValid(t *testing.T) {
	data := Generate(Config{Customers: 100, Orders: 2000, Seed: 1})

	if len(data.Customers) != 100 || len(data.Orders) != 2000 {
		t.Fatalf("customers = %d, orders = %d", len(data.Customers), len(data.Orders))
	}

	signups := map[int64]Customer{}
	for _, c := range data.Customers {
		signups[c.ID] = c
	}
	for _, o := range data.Orders {
		c, ok := signups[o.CustomerID]
		if !ok {
			t.Fatalf("order %d has unknown customer %d", o.ID, o.CustomerID)
		}
		if o.CreatedAt.Before(c.CreatedAt) {
			t.Errorf("order %d was made before the customer signed up", o.ID)
		}
		if !o.CreatedAt.Before(periodEnd) {
			t.Errorf("order %d is after the end of the period", o.ID)
		}
	}

	type key struct{ order, product int64 }
	seen := map[key]bool{}
	for _, i := range data.Items {
		k := key{i.OrderID, i.ProductID}
		if seen[k] {
			t.Errorf("duplicate item %+v", k)
		}
		seen[k] = true
	}
}
