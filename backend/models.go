package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"time"
)

type Money int64 // Céntimos; JSON decimal compatible con PostgreSQL numeric.
func (m Money) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf("%d.%02d", m/100, abs(int64(m%100)))), nil
}
func abs(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}
func (m *Money) UnmarshalJSON(b []byte) error {
	b = bytes.Trim(b, "\"")
	r, ok := new(big.Rat).SetString(string(b))
	if !ok {
		return fmt.Errorf("importe inválido")
	}
	r.Mul(r, big.NewRat(100, 1))
	if !r.IsInt() || !r.Num().IsInt64() {
		return fmt.Errorf("importe debe tener hasta dos decimales")
	}
	v := r.Num().Int64()
	if v < 0 || v > 100000000000 {
		return fmt.Errorf("importe fuera de rango")
	}
	*m = Money(v)
	return nil
}

type User struct {
	ID    string `json:"id"`
	Name  string `json:"full_name"`
	Phone string `json:"phone"`
	Email string `json:"email"`
	Role  string `json:"role"`
}
type Product struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Price       Money  `json:"price"`
	Category    string `json:"category"`
	Stock       int    `json:"available_stock"`
	Available   bool   `json:"is_available"`
	Image       string `json:"image_url,omitempty"`
	Updated     string `json:"updated_at,omitempty"`
}
type Line struct {
	ProductID string `json:"menu_item_id"`
	Name      string `json:"menu_item_name,omitempty"`
	Price     Money  `json:"unit_price"`
	Quantity  int    `json:"quantity"`
	Notes     string `json:"notes"`
}
type Payment struct {
	Method    string `json:"method"`
	Amount    Money  `json:"amount"`
	Reference string `json:"reference"`
	Created   string `json:"created_at"`
}
type Order struct {
	ID         string   `json:"id"`
	Number     int      `json:"order_number"`
	CustomerID string   `json:"customer_id"`
	Name       string   `json:"customer_name"`
	Phone      string   `json:"customer_phone"`
	Total      Money    `json:"total_amount"`
	Status     string   `json:"status"`
	Minutes    int      `json:"estimated_minutes"`
	Created    string   `json:"created_at"`
	Updated    string   `json:"updated_at"`
	Items      []Line   `json:"order_items"`
	Payment    *Payment `json:"payments"`
}

// PostgREST puede representar una relación 1:1 como objeto o arreglo.
func (o *Order) UnmarshalJSON(b []byte) error {
	type alias Order
	var raw struct {
		*alias
		Payment json.RawMessage `json:"payments"`
	}
	raw.alias = (*alias)(o)
	if e := json.Unmarshal(b, &raw); e != nil {
		return e
	}
	if len(raw.Payment) == 0 || string(raw.Payment) == "null" {
		o.Payment = nil
		return nil
	}
	if raw.Payment[0] == '[' {
		var p []Payment
		if e := json.Unmarshal(raw.Payment, &p); e != nil {
			return e
		}
		if len(p) > 0 {
			o.Payment = &p[0]
		}
		return nil
	}
	return json.Unmarshal(raw.Payment, &o.Payment)
}

type Announcement struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Message string `json:"message"`
	Created string `json:"created_at"`
}
type Closure struct {
	Date  string `json:"business_date"`
	Total Money  `json:"total_amount"`
	Count int    `json:"payment_count"`
}
type Snapshot struct {
	Archived      []Product      `json:"archived_menu"`
	Menu          []Product      `json:"menu"`
	Orders        []Order        `json:"orders"`
	Announcements []Announcement `json:"announcements"`
	Accepting     bool           `json:"accepting_orders"`
	Closures      []Closure      `json:"closures"`
}
type Checkout struct {
	RequestID string `json:"request_id"`
	Items     []Line `json:"items"`
}

var lima = time.FixedZone("America/Lima", -5*60*60)

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }
func businessDate(s string) string {
	t, e := time.Parse(time.RFC3339Nano, s)
	if e != nil {
		return ""
	}
	return t.In(lima).Format("2006-01-02")
}
func today() string { return time.Now().In(lima).Format("2006-01-02") }
