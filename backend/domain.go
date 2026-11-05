package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

type Receipt struct {
	OrderID string `json:"order_id"`
	Payload string `json:"payload"`
}
type BusinessState struct {
	Archives      map[string]bool    `json:"archives"`
	Reasons       map[string]string  `json:"-"`
	Menu          []Product          `json:"menu"`
	Orders        []Order            `json:"orders"`
	Announcements []Announcement     `json:"announcements"`
	Closures      []Closure          `json:"closures"`
	Accepting     bool               `json:"accepting"`
	Requests      map[string]Receipt `json:"requests"`
	Next          int                `json:"next"`
}

func demoUser(role string) User {
	switch role {
	case "ADMIN":
		return User{"10000000-0000-4000-8000-000000000003", "Adriana Torres", "999111003", "admin@demo.local", role}
	case "COOK":
		return User{"10000000-0000-4000-8000-000000000002", "Marco Ramos", "999111002", "cocina@demo.local", role}
	default:
		return User{"10000000-0000-4000-8000-000000000001", "Valentina Rojas", "999111001", "cliente@demo.local", "CLIENT"}
	}
}

func seedDemoState() BusinessState {
	st := emptyState()
	st.Next = 108
	names := []struct {
		name, desc, cat, img string
		price                Money
		stock                int
	}{
		{"Quinua de la casa", "Quinua suave con manzana, canela y ese sabor de casa.", "BEVERAGE", "/food-quinua.svg", 350, 32},
		{"Pan con palta", "Palta fresca, un toque de sal y pan crujiente del día.", "BREAD", "/food-palta.svg", 500, 24},
		{"El buen comienzo", "Tu bebida favorita y un pan con huevo recién hecho.", "COMBO", "/food-combo.svg", 850, 18},
		{"Pan con pollo", "Pollo deshilachado, lechuga fresca y mayonesa de casa.", "BREAD", "/food-pollo.svg", 650, 16},
		{"Maca con canela", "Calientita y cremosa. Energía para empezar tu mañana.", "BEVERAGE", "/food-maca.svg", 350, 28},
		{"Soya tradicional", "Una receta de siempre, servida a la temperatura perfecta.", "BEVERAGE", "/food-soya.svg", 300, 20},
		{"Pan con huevo", "Huevo a la plancha, pan fresco y un toque de pimienta.", "BREAD", "/food-huevo.svg", 450, 25},
		{"Porción de queso", "Queso fresco para acompañar tu desayuno favorito.", "EXTRA", "/food-queso.svg", 200, 15},
	}
	for i, p := range names {
		st.Menu = append(st.Menu, Product{fmt.Sprintf("20000000-0000-4000-8000-%012d", i+1), p.name, p.desc, p.price, p.cat, p.stock, true, p.img, now()})
	}
	u := demoUser("CLIENT")
	for i, status := range []string{"PENDING", "PREPARING", "READY", "PENDING", "DELIVERED", "DELIVERED"} {
		p := st.Menu[i%len(st.Menu)]
		stamp := time.Now().Add(time.Duration(-i*7-3) * time.Minute).UTC().Format(time.RFC3339Nano)
		o := Order{ID: randomID(), Number: 102 + i, CustomerID: u.ID, Name: u.Name, Phone: u.Phone, Total: p.Price * 2, Status: status, Minutes: 6, Created: stamp, Updated: stamp, Items: []Line{{p.ID, p.Name, p.Price, 2, ""}}}
		st.Menu[i%len(st.Menu)].Stock -= 2
		if i == 3 {
			o.Name = "Diego Mendoza"
			o.CustomerID = "10000000-0000-4000-8000-000000000004"
		}
		if status == "DELIVERED" {
			o.Payment = &Payment{"YAPE", o.Total, "DEMO-" + fmt.Sprint(i), stamp}
		}
		st.Orders = append(st.Orders, o)
	}
	st.Announcements = append(st.Announcements, Announcement{randomID(), "Una buena mañana empieza aquí", "Preparamos cada desayuno al momento. Haz tu pedido y recógelo en el local.", now()})
	return st
}
func canonical(items []Line) (string, error) {
	if len(items) < 1 || len(items) > 100 {
		return "", bad("El carrito debe tener entre 1 y 100 líneas")
	}
	m := map[string]Line{}
	for _, l := range items {
		if !validID(l.ProductID) || l.Quantity < 1 || l.Quantity > 100 || len([]rune(l.Notes)) > 500 {
			return "", bad("Producto, cantidad u observaciones inválidos")
		}
		k := l.ProductID + "\x00" + l.Notes
		v := m[k]
		v.ProductID = l.ProductID
		v.Notes = l.Notes
		v.Quantity += l.Quantity
		if v.Quantity > 100 {
			return "", bad("Cantidad máxima excedida")
		}
		m[k] = v
	}
	out := []Line{}
	for _, l := range m {
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ProductID == out[j].ProductID {
			return out[i].Notes < out[j].Notes
		}
		return out[i].ProductID < out[j].ProductID
	})
	b, _ := json.Marshal(out)
	return string(b), nil
}
func (d *Store) create(u User, c Checkout) (any, error) {
	return d.mutateAs(u.ID, func(st *BusinessState) (any, error) {
		if !validID(c.RequestID) {
			return nil, bad("request_id debe ser un UUID")
		}
		payload, e := canonical(c.Items)
		if e != nil {
			return nil, e
		}
		key := u.ID + ":" + c.RequestID
		if receipt, ok := st.Requests[key]; ok {
			if receipt.Payload != payload {
				return nil, &APIError{409, "El identificador pertenece a otro carrito"}
			}
			for _, o := range st.Orders {
				if o.ID == receipt.OrderID {
					return o, nil
				}
			}
		}
		if !st.Accepting {
			return nil, bad("La recepción de pedidos está cerrada")
		}
		if strings.TrimSpace(u.Name) == "" || strings.TrimSpace(u.Phone) == "" {
			return nil, bad("Complete su nombre y teléfono")
		}
		var lines []Line
		_ = json.Unmarshal([]byte(payload), &lines)
		qty := map[string]int{}
		for _, l := range lines {
			qty[l.ProductID] += l.Quantity
		}
		products := map[string]Product{}
		for _, p := range st.Menu {
			products[p.ID] = p
		}
		for id, n := range qty {
			p, ok := products[id]
			if !ok || st.Archives[id] || !p.Available || p.Stock < n || n > 100 {
				return nil, bad("Producto agotado o no disponible. Actualice su carrito.")
			}
		}
		o := Order{ID: randomID(), Number: st.Next, CustomerID: u.ID, Name: u.Name, Phone: u.Phone, Status: "PENDING", Created: now(), Updated: now(), Items: lines, Minutes: 3}
		for i := range o.Items {
			p := products[o.Items[i].ProductID]
			o.Items[i].Name = p.Name
			o.Items[i].Price = p.Price
			o.Total += p.Price * Money(o.Items[i].Quantity)
		}
		for i := range st.Menu {
			st.Menu[i].Stock -= qty[st.Menu[i].ID]
			if qty[st.Menu[i].ID] > 0 {
				st.Menu[i].Updated = now()
			}
		}
		for _, old := range st.Orders {
			if old.Status == "PENDING" || old.Status == "PREPARING" {
				o.Minutes += 3
			}
		}
		st.Next++
		st.Orders = append(st.Orders, o)
		st.Requests[key] = Receipt{o.ID, payload}
		return o, nil
	})
}
func canAccess(u User, o Order) bool { return u.Role != "CLIENT" || u.ID == o.CustomerID }
func payment(st *BusinessState, u User, o *Order, method, reference string) error {
	if u.Role != "ADMIN" {
		return denied()
	}
	if o.Status == "CANCELLED" {
		return bad("No se puede cobrar un pedido cancelado")
	}
	if method != "CASH" && method != "YAPE" && method != "PLIN" {
		return bad("Seleccione un método de pago")
	}
	reference = strings.TrimSpace(reference)
	if method != "CASH" && reference == "" {
		return bad("Ingrese la referencia verificada del pago")
	}
	if o.Payment != nil {
		if o.Payment.Method != method || o.Payment.Reference != reference {
			return &APIError{409, "El pedido ya tiene otro pago confirmado"}
		}
		return nil
	}
	for _, c := range st.Closures {
		if c.Date == today() {
			return bad("La caja de esta jornada ya está cerrada")
		}
	}
	o.Payment = &Payment{method, o.Total, reference, now()}
	return nil
}
func (d *Store) orderAction(u User, id, action, status, reason, method, reference string) (any, error) {
	return d.mutateAs(u.ID, func(st *BusinessState) (any, error) {
		idx := -1
		for i := range st.Orders {
			if st.Orders[i].ID == id && canAccess(u, st.Orders[i]) {
				idx = i
				break
			}
		}
		if idx < 0 {
			return nil, &APIError{404, "Pedido no encontrado"}
		}
		o := &st.Orders[idx]
		if action == "payment" {
			if e := payment(st, u, o, method, reference); e != nil {
				return nil, e
			}
			return *o, nil
		}
		if action == "complete" {
			if u.Role == "CLIENT" {
				return nil, denied()
			}
			if o.Status != "READY" && o.Status != "DELIVERED" {
				return nil, bad("El pedido debe estar listo antes de entregarlo")
			}
			if o.Payment == nil {
				if e := payment(st, u, o, method, reference); e != nil {
					return nil, e
				}
			} else if method != "" {
				if e := payment(st, u, o, method, reference); e != nil {
					return nil, e
				}
			}
			status = "DELIVERED"
		}
		if o.Status == status {
			return *o, nil
		}
		if status == "CANCELLED" {
			if o.Status != "PENDING" || strings.TrimSpace(reason) == "" || o.Payment != nil {
				return nil, bad("Solo se cancelan pedidos en cola, sin pago y con motivo")
			}
			for _, l := range o.Items {
				for i := range st.Menu {
					if st.Menu[i].ID == l.ProductID {
						st.Menu[i].Stock += l.Quantity
						st.Menu[i].Updated = now()
					}
				}
			}
		} else {
			if u.Role == "CLIENT" {
				return nil, denied()
			}
			valid := (o.Status == "PENDING" && status == "PREPARING") || (o.Status == "PREPARING" && status == "READY") || (o.Status == "READY" && status == "DELIVERED")
			if !valid {
				return nil, bad("Transición de estado no permitida")
			}
			if status == "DELIVERED" && o.Payment == nil {
				return nil, bad("Registre el pago antes de entregar")
			}
		}
		if st.Reasons == nil {
			st.Reasons = map[string]string{}
		}
		st.Reasons[o.ID] = reason
		o.Status = status
		o.Updated = now()
		return *o, nil
	})
}
