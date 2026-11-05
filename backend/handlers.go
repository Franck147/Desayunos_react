package main

import (
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	v, e := s.dispatch(w, r)
	if e != nil {
		writeError(w, e)
		return
	}
	if v != nil {
		writeJSON(w, 200, v)
	}
}
func (s *Server) dispatch(w http.ResponseWriter, r *http.Request) (any, error) {
	p := strings.TrimPrefix(r.URL.Path, "/api/v1")
	method := r.Method
	if strings.HasPrefix(p, "/auth/") && method == "POST" && p != "/auth/logout" {
		if !s.allowAuth(r) {
			return nil, &APIError{429, "Demasiados intentos. Espere un minuto."}
		}
	}
	if p == "/auth/login" && method == "POST" {
		var input struct {
			Email    string `json:"email"`
			Password string `json:"password"`
			Staff    bool   `json:"staff"`
			Access   string `json:"access"`
		}
		if e := decode(r, &input); e != nil {
			return nil, e
		}
		if len(input.Password) > 128 {
			return nil, bad("Credenciales inválidas")
		}
		identifier := strings.ToLower(strings.TrimSpace(input.Email))
		if !strings.Contains(identifier, "@") {
			identifier += "@demo.local"
		}
		var u User
		var hash string
		e := s.store.db.QueryRow("SELECT id,full_name,phone,email,role,password_hash FROM users WHERE email=? COLLATE NOCASE", identifier).Scan(&u.ID, &u.Name, &u.Phone, &u.Email, &u.Role, &hash)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return nil, e
		}
		if e != nil {
			_, _ = hashPassword(input.Password)
			return nil, &APIError{401, "Correo o contraseña incorrectos"}
		}
		if !checkPassword(hash, input.Password) {
			return nil, &APIError{401, "Correo o contraseña incorrectos"}
		}
		if input.Access != "" {
			if input.Access != "CLIENT" && input.Access != "COOK" && input.Access != "ADMIN" {
				return nil, bad("Tipo de acceso inválido")
			}
			if input.Access != u.Role {
				return nil, &APIError{403, "Esta cuenta no corresponde al acceso seleccionado. Elige Cliente, Usuario de cocina o Administrador según tu cuenta."}
			}
		}
		if input.Staff && u.Role == "CLIENT" {
			return nil, denied()
		}
		return u, s.createSession(w, r, u)
	}
	if p == "/auth/signup" && method == "POST" {
		var input struct {
			Name     string `json:"name"`
			Phone    string `json:"phone"`
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if e := decode(r, &input); e != nil {
			return nil, e
		}
		u, e := s.store.createUser(input.Name, input.Phone, input.Email, input.Password, "CLIENT")
		if e != nil {
			return nil, e
		}
		if e = s.createSession(w, r, u); e != nil {
			return nil, e
		}
		return map[string]any{"user": u, "confirmation_required": false}, nil
	}
	if p == "/auth/demo" && method == "POST" {
		if s.cfg.Mode != "demo" {
			return nil, &APIError{404, "Ruta no disponible"}
		}
		var input struct {
			Role string `json:"role"`
		}
		if e := decode(r, &input); e != nil {
			return nil, e
		}
		if input.Role != "CLIENT" && input.Role != "COOK" && input.Role != "ADMIN" {
			return nil, bad("Rol inválido")
		}
		u := demoUser(input.Role)
		return u, s.createSession(w, r, u)
	}
	if p == "/auth/logout" && method == "POST" {
		if c, e := r.Cookie("desayunos_session"); e == nil {
			if _, e = s.store.db.Exec("DELETE FROM sessions WHERE token_hash=?", tokenHash(c.Value)); e != nil {
				return nil, e
			}
		}
		http.SetCookie(w, &http.Cookie{Name: "desayunos_session", Path: "/", HttpOnly: true, Secure: s.cfg.Secure, SameSite: http.SameSiteLaxMode, MaxAge: -1})
		return map[string]bool{"ok": true}, nil
	}
	session, e := s.session(r)
	if e != nil {
		return nil, e
	}
	u := session.User
	if p == "/me" && method == "GET" {
		return u, nil
	}
	if p == "/me" && method == "PATCH" {
		var input struct {
			Name  string `json:"full_name"`
			Phone string `json:"phone"`
		}
		if e = decode(r, &input); e != nil {
			return nil, e
		}
		input.Name = strings.TrimSpace(input.Name)
		input.Phone = strings.TrimSpace(input.Phone)
		if input.Name == "" || input.Phone == "" || len(input.Name) > 150 || len(input.Phone) > 40 {
			return nil, bad("Ingrese nombre y teléfono válidos")
		}
		_, e = s.store.db.Exec("UPDATE users SET full_name=?,phone=? WHERE id=?", input.Name, input.Phone, u.ID)
		u.Name = input.Name
		u.Phone = input.Phone
		return u, e
	}
	if p == "/snapshot" && method == "GET" {
		return s.store.snapshot(u)
	}
	if p == "/orders" && method == "POST" {
		var c Checkout
		if e = decode(r, &c); e != nil {
			return nil, e
		}
		return s.store.create(u, c)
	}
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) == 3 && parts[0] == "orders" && method == "POST" {
		id, action := parts[1], parts[2]
		if !validID(id) {
			return nil, bad("Pedido inválido")
		}
		var input struct {
			Status    string `json:"status"`
			Reason    string `json:"reason"`
			Method    string `json:"method"`
			Reference string `json:"reference"`
		}
		if e = decode(r, &input); e != nil {
			return nil, e
		}
		if action != "status" && action != "payment" && action != "complete" {
			return nil, &APIError{404, "Operación no encontrada"}
		}
		if action == "payment" && u.Role != "ADMIN" {
			return nil, denied()
		}
		if action == "complete" && u.Role == "CLIENT" {
			return nil, denied()
		}
		if len(input.Reference) > 200 || len(input.Reason) > 500 {
			return nil, bad("Referencia o motivo demasiado largo")
		}
		return s.store.orderAction(u, id, action, input.Status, input.Reason, input.Method, input.Reference)
	}
	if (len(parts) == 2 && parts[0] == "menu" && method == "DELETE") || (len(parts) == 3 && parts[0] == "menu" && parts[2] == "restore" && method == "POST") {
		if !canManageMenu(u) {
			return nil, denied()
		}
		var input struct {
			Expected string `json:"expected_updated_at"`
		}
		if e = decode(r, &input); e != nil {
			return nil, e
		}
		return s.store.archiveProduct(u, parts[1], input.Expected, method == "POST")
	}
	if p == "/menu" && method == "PUT" {
		if !canManageMenu(u) {
			return nil, denied()
		}
		var item Product
		if e = decode(r, &item); e != nil {
			return nil, e
		}
		if !validID(item.ID) || strings.TrimSpace(item.Name) == "" || len(item.Name) > 150 || len(item.Description) > 2000 || item.Stock < 0 || item.Stock > 1000000 {
			return nil, bad("Revise nombre, descripción y stock")
		}
		if item.Category != "BREAD" && item.Category != "BEVERAGE" && item.Category != "EXTRA" && item.Category != "COMBO" {
			return nil, bad("Categoría inválida")
		}
		if item.Image != "" && !strings.HasPrefix(item.Image, "/food-") && !strings.HasPrefix(item.Image, "/api/v1/media/") {
			uri, err := url.Parse(item.Image)
			if err != nil || uri.Scheme != "https" || uri.Host == "" {
				return nil, bad("La imagen debe usar una URL HTTPS")
			}
		}
		return s.store.mutateAs(u.ID, func(st *BusinessState) (any, error) {
			for i, old := range st.Menu {
				if old.ID == item.ID {
					if st.Archives[item.ID] {
						return nil, &APIError{409, "Restaura el desayuno antes de editarlo."}
					}
					if old.Updated != item.Updated {
						return nil, &APIError{409, "El producto cambió. Actualice antes de editar."}
					}
					item.Updated = now()
					st.Menu[i] = item
					return item, nil
				}
			}
			item.Updated = now()
			st.Menu = append(st.Menu, item)
			return item, nil
		})
	}
	if p == "/announcements" && method == "POST" {
		if u.Role != "ADMIN" {
			return nil, denied()
		}
		var input struct {
			Title   string `json:"title"`
			Message string `json:"message"`
		}
		if e = decode(r, &input); e != nil {
			return nil, e
		}
		input.Title = strings.TrimSpace(input.Title)
		input.Message = strings.TrimSpace(input.Message)
		if input.Title == "" || len([]rune(input.Title)) > 150 || input.Message == "" || len([]rune(input.Message)) > 2000 {
			return nil, bad("El aviso requiere título (hasta 150 caracteres) y mensaje (hasta 2000)")
		}
		return s.store.mutateAs(u.ID, func(st *BusinessState) (any, error) {
			a := Announcement{randomID(), input.Title, input.Message, now()}
			st.Announcements = append([]Announcement{a}, st.Announcements...)
			return a, nil
		})
	}
	if p == "/business-settings" && method == "PATCH" {
		if u.Role != "ADMIN" {
			return nil, denied()
		}
		var input struct {
			Accepting *bool `json:"accepting_orders"`
		}
		if e = decode(r, &input); e != nil {
			return nil, e
		}
		if input.Accepting == nil {
			return nil, bad("Indique si se reciben pedidos")
		}
		return s.store.mutateAs(u.ID, func(st *BusinessState) (any, error) { st.Accepting = *input.Accepting; return input, nil })
	}
	if p == "/cash-closures" && method == "POST" {
		if u.Role != "ADMIN" {
			return nil, denied()
		}
		var input struct {
			Date  string `json:"date"`
			Total Money  `json:"expected_total"`
		}
		if e = decode(r, &input); e != nil {
			return nil, e
		}
		if !validDate(input.Date) || input.Date > today() {
			return nil, bad("Fecha de cierre inválida")
		}
		return s.store.mutateAs(u.ID, func(st *BusinessState) (any, error) {
			for _, c := range st.Closures {
				if c.Date == input.Date {
					return c, nil
				}
			}
			report := buildReport(st.Orders, input.Date)
			if report.Revenue != input.Total {
				return nil, &APIError{409, "Los cobros cambiaron. Actualice antes de cerrar."}
			}
			c := Closure{input.Date, report.Revenue, report.PaidOrders}
			st.Closures = append(st.Closures, c)
			return c, nil
		})
	}
	if (p == "/reports/daily" || p == "/reports/daily.csv") && method == "GET" {
		if u.Role == "CLIENT" {
			return nil, denied()
		}
		date := r.URL.Query().Get("date")
		if !validDate(date) {
			return nil, bad("Fecha inválida")
		}
		snapshot, e := s.store.snapshot(u)
		if e != nil {
			return nil, e
		}
		report := buildReport(snapshot.Orders, date)
		if strings.HasSuffix(p, ".csv") {
			w.Header().Set("Content-Type", "text/csv; charset=utf-8")
			w.Header().Set("Content-Disposition", `attachment; filename="Desayunos_`+date+`.csv"`)
			writeCSV(w, report)
			return nil, nil
		}
		return report, nil
	}
	if p == "/images" && method == "POST" {
		if !canManageMenu(u) {
			return nil, denied()
		}
		return s.upload(r)
	}
	if p == "/users" && method == "GET" {
		if u.Role != "ADMIN" {
			return nil, denied()
		}
		rows, e := s.store.db.Query("SELECT id,full_name,phone,email,role FROM users ORDER BY full_name")
		if e != nil {
			return nil, e
		}
		defer rows.Close()
		users := []User{}
		for rows.Next() {
			var v User
			if e = rows.Scan(&v.ID, &v.Name, &v.Phone, &v.Email, &v.Role); e != nil {
				return nil, e
			}
			users = append(users, v)
		}
		return users, rows.Err()
	}
	if len(parts) == 2 && parts[0] == "users" && method == "PATCH" {
		if u.Role != "ADMIN" {
			return nil, denied()
		}
		var input struct {
			Role string `json:"role"`
		}
		if e = decode(r, &input); e != nil {
			return nil, e
		}
		if !validID(parts[1]) || (input.Role != "ADMIN" && input.Role != "COOK" && input.Role != "CLIENT") {
			return nil, bad("Usuario o rol inválido")
		}
		if parts[1] == u.ID && input.Role != "ADMIN" {
			return nil, bad("No puede retirar sus propios permisos administrativos")
		}
		result, e := s.store.db.Exec("UPDATE users SET role=? WHERE id=?", input.Role, parts[1])
		if e != nil {
			return nil, e
		}
		n, _ := result.RowsAffected()
		if n == 0 {
			return nil, &APIError{404, "Usuario no encontrado"}
		}
		return map[string]bool{"ok": true}, nil
	}
	return nil, &APIError{404, "Ruta no encontrada"}
}

var _ = time.Now
