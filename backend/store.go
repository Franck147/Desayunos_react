package main

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"strings"
	"time"
)

//go:embed schema.sql
var schema string

type Store struct{ db *sql.DB }

func emptyState() BusinessState {
	return BusinessState{Menu: []Product{}, Orders: []Order{}, Announcements: []Announcement{}, Closures: []Closure{}, Requests: map[string]Receipt{}, Accepting: true, Next: 101}
}
func openStore(path string, demo bool) (*Store, error) {
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return nil, e
	}
	dsn := filepath.ToSlash(path) + "?_pragma=busy_timeout(10000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_txlock=immediate"
	db, e := sql.Open("sqlite", dsn)
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	s := &Store{db}
	if _, e = db.Exec(schema); e != nil {
		db.Close()
		return nil, e
	}
	if demo {
		var count int
		if e = db.QueryRow("SELECT count(*) FROM users").Scan(&count); e != nil {
			return nil, e
		}
		if count == 0 {
			for _, role := range []string{"CLIENT", "COOK", "ADMIN"} {
				u := demoUser(role)
				_, e = db.Exec("INSERT INTO users VALUES(?,?,?,?,?,?,?)", u.ID, u.Email, u.Name, u.Phone, u.Role, "!demo-disabled-password", now())
				if e != nil {
					return nil, e
				}
			}
			_, e = db.Exec("INSERT INTO users VALUES(?,?,?,?,?,?,?)", "10000000-0000-4000-8000-000000000004", "diego@demo.local", "Diego Mendoza", "999111004", "CLIENT", "!demo-disabled-password", now())
			if e != nil {
				return nil, e
			}
			_, e = s.mutateAs(demoUser("ADMIN").ID, func(st *BusinessState) (any, error) { *st = seedDemoState(); return nil, nil })
			if e != nil {
				return nil, e
			}
		}
	}
	return s, nil
}
func (s *Store) load(tx *sql.Tx) (BusinessState, error) {
	st := emptyState()
	st.Archives = map[string]bool{}
	archiveRows, err := tx.Query("SELECT menu_item_id FROM menu_item_archives")
	if err != nil {
		return st, err
	}
	for archiveRows.Next() {
		var id string
		if err = archiveRows.Scan(&id); err != nil {
			archiveRows.Close()
			return st, err
		}
		st.Archives[id] = true
	}
	if err = archiveRows.Err(); err != nil {
		archiveRows.Close()
		return st, err
	}
	archiveRows.Close()
	if e := tx.QueryRow("SELECT accepting_orders,next_order_number FROM business_settings WHERE id=1").Scan(&st.Accepting, &st.Next); e != nil {
		return st, e
	}
	rows, e := tx.Query("SELECT id,name,description,price_cents,category,available_stock,is_available,image_url,updated_at FROM menu_items ORDER BY name,id")
	if e != nil {
		return st, e
	}
	for rows.Next() {
		var p Product
		if e = rows.Scan(&p.ID, &p.Name, &p.Description, &p.Price, &p.Category, &p.Stock, &p.Available, &p.Image, &p.Updated); e != nil {
			rows.Close()
			return st, e
		}
		st.Menu = append(st.Menu, p)
	}
	if e = rows.Err(); e != nil {
		return st, e
	}
	rows.Close()
	rows, e = tx.Query("SELECT id,order_number,customer_id,customer_name,customer_phone,total_cents,status,estimated_minutes,created_at,updated_at FROM orders ORDER BY created_at DESC,id DESC")
	if e != nil {
		return st, e
	}
	index := map[string]int{}
	for rows.Next() {
		var o Order
		if e = rows.Scan(&o.ID, &o.Number, &o.CustomerID, &o.Name, &o.Phone, &o.Total, &o.Status, &o.Minutes, &o.Created, &o.Updated); e != nil {
			rows.Close()
			return st, e
		}
		o.Items = []Line{}
		index[o.ID] = len(st.Orders)
		st.Orders = append(st.Orders, o)
	}
	if e = rows.Err(); e != nil {
		return st, e
	}
	rows.Close()
	rows, e = tx.Query("SELECT order_id,menu_item_id,menu_item_name,unit_price_cents,quantity,notes FROM order_items ORDER BY id")
	if e != nil {
		return st, e
	}
	for rows.Next() {
		var id string
		var l Line
		if e = rows.Scan(&id, &l.ProductID, &l.Name, &l.Price, &l.Quantity, &l.Notes); e != nil {
			rows.Close()
			return st, e
		}
		i := index[id]
		st.Orders[i].Items = append(st.Orders[i].Items, l)
	}
	if e = rows.Err(); e != nil {
		return st, e
	}
	rows.Close()
	rows, e = tx.Query("SELECT order_id,method,amount_cents,reference,created_at FROM payments")
	if e != nil {
		return st, e
	}
	for rows.Next() {
		var id string
		var p Payment
		if e = rows.Scan(&id, &p.Method, &p.Amount, &p.Reference, &p.Created); e != nil {
			rows.Close()
			return st, e
		}
		st.Orders[index[id]].Payment = &p
	}
	if e = rows.Err(); e != nil {
		return st, e
	}
	rows.Close()
	rows, e = tx.Query("SELECT id,title,message,created_at FROM announcements ORDER BY created_at DESC,id DESC")
	if e != nil {
		return st, e
	}
	for rows.Next() {
		var a Announcement
		if e = rows.Scan(&a.ID, &a.Title, &a.Message, &a.Created); e != nil {
			rows.Close()
			return st, e
		}
		st.Announcements = append(st.Announcements, a)
	}
	if e = rows.Err(); e != nil {
		return st, e
	}
	rows.Close()
	rows, e = tx.Query("SELECT business_date,total_cents,payment_count FROM cash_closures ORDER BY business_date DESC")
	if e != nil {
		return st, e
	}
	for rows.Next() {
		var c Closure
		if e = rows.Scan(&c.Date, &c.Total, &c.Count); e != nil {
			rows.Close()
			return st, e
		}
		st.Closures = append(st.Closures, c)
	}
	if e = rows.Err(); e != nil {
		return st, e
	}
	rows.Close()
	rows, e = tx.Query("SELECT customer_id,request_id,order_id,payload FROM request_receipts")
	if e != nil {
		return st, e
	}
	for rows.Next() {
		var user, id string
		var receipt Receipt
		if e = rows.Scan(&user, &id, &receipt.OrderID, &receipt.Payload); e != nil {
			rows.Close()
			return st, e
		}
		st.Requests[user+":"+id] = receipt
	}
	if e = rows.Err(); e != nil {
		return st, e
	}
	rows.Close()
	return st, nil
}
func (s *Store) mutateAs(actor string, fn func(*BusinessState) (any, error)) (any, error) {
	tx, e := s.db.BeginTx(context.Background(), nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	st, e := s.load(tx)
	if e != nil {
		return nil, e
	}
	before, _ := json.Marshal(st)
	v, e := fn(&st)
	if e != nil {
		return nil, e
	}
	var old BusinessState
	if e = json.Unmarshal(before, &old); e != nil {
		return nil, e
	}
	if e = s.save(tx, old, st, actor); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return v, nil
}
func (s *Store) save(tx *sql.Tx, old, st BusinessState, actor string) error {
	oldProducts := map[string]Product{}
	for _, p := range old.Menu {
		oldProducts[p.ID] = p
	}
	oldOrders := map[string]Order{}
	for _, o := range old.Orders {
		oldOrders[o.ID] = o
	}
	// Todos los cambios de una operación comparten la misma transacción IMMEDIATE.
	orderID, reason := "", "ADJUSTMENT"
	for _, o := range st.Orders {
		prior, exists := oldOrders[o.ID]
		if !exists {
			orderID = o.ID
			reason = "ORDER"
		} else if prior.Status != o.Status && o.Status == "CANCELLED" {
			orderID = o.ID
			reason = "CANCELLATION"
		}
	}
	for _, p := range st.Menu {
		_, e := tx.Exec(`INSERT INTO menu_items VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,description=excluded.description,price_cents=excluded.price_cents,category=excluded.category,available_stock=excluded.available_stock,is_available=excluded.is_available,image_url=excluded.image_url,updated_at=excluded.updated_at`, p.ID, p.Name, p.Description, p.Price, p.Category, p.Stock, p.Available, p.Image, p.Updated)
		if e != nil {
			return e
		}
	}
	for _, o := range st.Orders {
		status := o.Status
		if status == "DELIVERED" {
			status = "READY"
		}
		_, e := tx.Exec("INSERT OR IGNORE INTO orders VALUES(?,?,?,?,?,?,?,?,?,?)", o.ID, o.Number, o.CustomerID, o.Name, o.Phone, o.Total, status, o.Minutes, o.Created, o.Updated)
		if e != nil {
			return e
		}
		if _, exists := oldOrders[o.ID]; !exists {
			for _, l := range o.Items {
				if _, e = tx.Exec("INSERT INTO order_items(order_id,menu_item_id,menu_item_name,unit_price_cents,quantity,notes) VALUES(?,?,?,?,?,?)", o.ID, l.ProductID, l.Name, l.Price, l.Quantity, l.Notes); e != nil {
					return e
				}
			}
		}
		if o.Payment != nil && oldOrders[o.ID].Payment == nil {
			_, e = tx.Exec("INSERT INTO payments VALUES(?,?,?,?,?,?) ON CONFLICT(order_id) DO NOTHING", o.ID, o.Payment.Method, o.Payment.Amount, o.Payment.Reference, actor, o.Payment.Created)
			if e != nil {
				return e
			}
		}
		if _, e = tx.Exec("UPDATE orders SET status=?,updated_at=? WHERE id=?", o.Status, o.Updated, o.ID); e != nil {
			return e
		}
		prior := oldOrders[o.ID]
		if prior.Status != o.Status {
			_, e = tx.Exec("INSERT INTO order_status_history(order_id,previous_status,new_status,actor_id,reason,created_at) VALUES(?,?,?,?,?,?)", o.ID, prior.Status, o.Status, actor, st.Reasons[o.ID], now())
			if e != nil {
				return e
			}
		}
	}
	for _, p := range st.Menu {
		prior, exists := oldProducts[p.ID]
		delta := p.Stock - prior.Stock
		if delta == 0 {
			continue
		}
		r := reason
		if !exists {
			r = "INITIAL"
		}
		var ref any
		if orderID != "" {
			ref = orderID
		}
		if _, e := tx.Exec("INSERT INTO stock_movements(menu_item_id,quantity_delta,reason,order_id,actor_id,created_at) VALUES(?,?,?,?,?,?)", p.ID, delta, r, ref, actor, now()); e != nil {
			return e
		}
	}
	for _, a := range st.Announcements {
		if _, e := tx.Exec("INSERT INTO announcements VALUES(?,?,?,?,?) ON CONFLICT(id) DO NOTHING", a.ID, a.Title, a.Message, actor, a.Created); e != nil {
			return e
		}
	}
	for _, c := range st.Closures {
		if _, e := tx.Exec("INSERT INTO cash_closures VALUES(?,?,?,?,?) ON CONFLICT(business_date) DO NOTHING", c.Date, c.Total, c.Count, actor, now()); e != nil {
			return e
		}
	}
	for key, r := range st.Requests {
		user, id, ok := strings.Cut(key, ":")
		if !ok {
			return fmt.Errorf("recibo inválido")
		}
		if _, e := tx.Exec("INSERT INTO request_receipts VALUES(?,?,?,?) ON CONFLICT(customer_id,request_id) DO NOTHING", user, id, r.Payload, r.OrderID); e != nil {
			return e
		}
	}
	_, e := tx.Exec("UPDATE business_settings SET accepting_orders=?,next_order_number=? WHERE id=1", st.Accepting, st.Next)
	return e
}
func (s *Store) snapshot(u User) (Snapshot, error) {
	tx, e := s.db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return Snapshot{}, e
	}
	defer tx.Rollback()
	st, e := s.load(tx)
	if e != nil {
		return Snapshot{}, e
	}
	out := Snapshot{Menu: []Product{}, Orders: []Order{}, Announcements: st.Announcements, Closures: []Closure{}, Accepting: st.Accepting}
	out.Archived = []Product{}
	for _, p := range st.Menu {
		if st.Archives[p.ID] {
			if canManageMenu(u) {
				out.Archived = append(out.Archived, p)
			}
			continue
		}
		if p.Available || u.Role != "CLIENT" {
			out.Menu = append(out.Menu, p)
		}
	}
	for _, o := range st.Orders {
		if canAccess(u, o) {
			out.Orders = append(out.Orders, o)
		}
	}
	if u.Role == "ADMIN" {
		out.Closures = st.Closures
	}
	return out, nil
}
func (s *Store) backup(path string) error {
	if strings.Contains(path, "'") {
		return bad("Ruta de backup inválida")
	}
	_, e := s.db.Exec("VACUUM INTO ?", path)
	return e
}
func (s *Store) integrity() error {
	var result string
	if e := s.db.QueryRow("PRAGMA integrity_check").Scan(&result); e != nil {
		return e
	}
	if result != "ok" {
		return fmt.Errorf("SQLite: %s", result)
	}
	return nil
}
func (s *Store) cleanupSessions() {
	_, _ = s.db.Exec("DELETE FROM sessions WHERE expires_at<?", time.Now().Unix())
}
