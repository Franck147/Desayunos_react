package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func testServer(t *testing.T, demo bool) *Server {
	t.Helper()
	dir := t.TempDir()
	mode := "sqlite"
	if demo {
		mode = "demo"
	}
	s, e := newServer(Config{Mode: mode, Origin: "http://127.0.0.1:3000", Data: dir, DBPath: filepath.Join(dir, "test.db")})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.store.db.Close() })
	return s
}
func request(t *testing.T, s *Server, method, path string, body any, cookie *http.Cookie, origin string) *httptest.ResponseRecorder {
	t.Helper()
	var b bytes.Buffer
	if body != nil {
		if e := json.NewEncoder(&b).Encode(body); e != nil {
			t.Fatal(e)
		}
	}
	r := httptest.NewRequest(method, "http://127.0.0.1:8080/api/v1"+path, &b)
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("Content-Type", "application/json")
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, r)
	return w
}
func demoLogin(t *testing.T, s *Server, role string) *http.Cookie {
	t.Helper()
	w := request(t, s, "POST", "/auth/demo", map[string]string{"role": role}, nil, s.cfg.Origin)
	if w.Code != 200 {
		t.Fatalf("login %d %s", w.Code, w.Body)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("falta cookie")
	}
	if !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatal("cookie insegura")
	}
	return cookies[0]
}
func snapshot(t *testing.T, s *Server, role string) Snapshot {
	t.Helper()
	v, e := s.store.snapshot(demoUser(role))
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func newOrder(t *testing.T, s *Server) (Order, Checkout) {
	t.Helper()
	p := snapshot(t, s, "CLIENT").Menu[0]
	c := Checkout{randomID(), []Line{{ProductID: p.ID, Quantity: 1, Notes: "Sin azúcar"}}}
	v, e := s.store.create(demoUser("CLIENT"), c)
	if e != nil {
		t.Fatal(e)
	}
	return v.(Order), c
}
func TestSQLiteIntegrityAndForeignKeys(t *testing.T) {
	s := testServer(t, true)
	if e := s.store.integrity(); e != nil {
		t.Fatal(e)
	}
	var fk int
	var journal string
	_ = s.store.db.QueryRow("PRAGMA foreign_keys").Scan(&fk)
	_ = s.store.db.QueryRow("PRAGMA journal_mode").Scan(&journal)
	if fk != 1 || journal != "wal" {
		t.Fatalf("foreign_keys=%d journal=%s", fk, journal)
	}
	_, e := s.store.db.Exec("INSERT INTO sessions VALUES('bad','unknown',9999999999)")
	if e == nil {
		t.Fatal("FK no aplicada")
	}
}
func TestCheckoutIdempotencyAndServerPrices(t *testing.T) {
	s := testServer(t, true)
	before := snapshot(t, s, "CLIENT")
	o, c := newOrder(t, s)
	c.Items[0].Price = 1
	v, e := s.store.create(demoUser("CLIENT"), c)
	if e != nil {
		t.Fatal(e)
	}
	if v.(Order).ID != o.ID {
		t.Fatal("pedido duplicado")
	}
	if o.Total != before.Menu[0].Price {
		t.Fatal("se aceptó precio del cliente")
	}
	after := snapshot(t, s, "CLIENT")
	if after.Menu[0].Stock != before.Menu[0].Stock-1 {
		t.Fatal("stock descontado más de una vez")
	}
	c.Items[0].Quantity = 2
	if _, e = s.store.create(demoUser("CLIENT"), c); e == nil {
		t.Fatal("reutilización de request_id aceptada")
	}
}
func TestConcurrentLastUnit(t *testing.T) {
	s := testServer(t, true)
	p := snapshot(t, s, "ADMIN").Menu[0]
	_, e := s.store.mutateAs(demoUser("ADMIN").ID, func(st *BusinessState) (any, error) {
		for i := range st.Menu {
			if st.Menu[i].ID == p.ID {
				st.Menu[i].Stock = 1
			}
		}
		return nil, nil
	})
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.store.create(demoUser("CLIENT"), Checkout{randomID(), []Line{{ProductID: p.ID, Quantity: 1}}})
			if err == nil {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("se confirmaron %d pedidos para una unidad", wins)
	}
	for _, item := range snapshot(t, s, "ADMIN").Menu {
		if item.ID == p.ID && item.Stock != 0 {
			t.Fatal("stock incorrecto")
		}
	}
}
func TestCancellationRestoresOnceAndHasAudit(t *testing.T) {
	s := testServer(t, true)
	o, _ := newOrder(t, s)
	before := snapshot(t, s, "ADMIN")
	for i := 0; i < 2; i++ {
		if _, e := s.store.orderAction(demoUser("CLIENT"), o.ID, "status", "CANCELLED", "Cambié de opinión", "", ""); e != nil {
			t.Fatal(e)
		}
	}
	after := snapshot(t, s, "ADMIN")
	for i, p := range after.Menu {
		if p.ID == o.Items[0].ProductID && p.Stock != before.Menu[i].Stock+1 {
			t.Fatal("devolución incorrecta")
		}
	}
	var n int
	_ = s.store.db.QueryRow("SELECT count(*) FROM order_status_history WHERE order_id=? AND new_status='CANCELLED'", o.ID).Scan(&n)
	if n != 1 {
		t.Fatal("historial duplicado")
	}
}
func TestAtomicCompletionRollbackAndIdempotency(t *testing.T) {
	s := testServer(t, true)
	o, _ := newOrder(t, s)
	for _, status := range []string{"PREPARING", "READY"} {
		if _, e := s.store.orderAction(demoUser("COOK"), o.ID, "status", status, "", "", ""); e != nil {
			t.Fatal(e)
		}
	}
	_, e := s.store.db.Exec(`CREATE TRIGGER fail_delivery BEFORE UPDATE OF status ON orders WHEN NEW.id='` + o.ID + `' AND NEW.status='DELIVERED' BEGIN SELECT RAISE(ABORT,'failure');END;`)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.store.orderAction(demoUser("ADMIN"), o.ID, "complete", "", "", "YAPE", "123"); e == nil {
		t.Fatal("no se simuló fallo")
	}
	var count int
	_ = s.store.db.QueryRow("SELECT count(*) FROM payments WHERE order_id=?", o.ID).Scan(&count)
	if count != 0 {
		t.Fatal("cobro sobrevivió al fallo de entrega")
	}
	_, _ = s.store.db.Exec("DROP TRIGGER fail_delivery")
	for i := 0; i < 2; i++ {
		v, e := s.store.orderAction(demoUser("ADMIN"), o.ID, "complete", "", "", "YAPE", "123")
		if e != nil {
			t.Fatal(e)
		}
		if v.(Order).Status != "DELIVERED" || v.(Order).Payment == nil {
			t.Fatal("entrega incompleta")
		}
	}
	_ = s.store.db.QueryRow("SELECT count(*) FROM payments WHERE order_id=?", o.ID).Scan(&count)
	if count != 1 {
		t.Fatal("pago duplicado")
	}
}
func TestRoleAndOwnershipEnforced(t *testing.T) {
	s := testServer(t, true)
	client := demoLogin(t, s, "CLIENT")
	cook := demoLogin(t, s, "COOK")
	o, _ := newOrder(t, s)
	checks := []struct {
		method, path string
		body         any
		cookie       *http.Cookie
		code         int
	}{{"PUT", "/menu", snapshot(t, s, "ADMIN").Menu[0], client, 403}, {"POST", "/cash-closures", map[string]any{"date": today(), "expected_total": 0}, cook, 403}, {"POST", "/orders/" + o.ID + "/payment", map[string]string{"method": "CASH"}, cook, 403}, {"POST", "/orders/" + o.ID + "/status", map[string]string{"status": "PREPARING"}, client, 403}, {"GET", "/reports/daily?date=" + today(), nil, client, 403}, {"PATCH", "/me", map[string]string{"role": "ADMIN"}, client, 400}, {"PATCH", "/business-settings", map[string]bool{"accepting_orders": false}, client, 403}}
	for _, c := range checks {
		w := request(t, s, c.method, c.path, c.body, c.cookie, s.cfg.Origin)
		if w.Code != c.code {
			t.Errorf("%s %s: %d %s", c.method, c.path, w.Code, w.Body)
		}
	}
	other := demoUser("CLIENT")
	other.ID = "10000000-0000-4000-8000-000000000004"
	if _, e := s.store.orderAction(other, o.ID, "status", "CANCELLED", "No es mío", "", ""); e == nil {
		t.Fatal("otro cliente canceló pedido")
	}
	visible, e := s.store.snapshot(other)
	if e != nil {
		t.Fatal(e)
	}
	for _, x := range visible.Orders {
		if x.ID == o.ID {
			t.Fatal("filtrado por propietario roto")
		}
	}
}
func TestOriginAndSessionRevocation(t *testing.T) {
	s := testServer(t, true)
	w := request(t, s, "POST", "/auth/demo", map[string]string{"role": "ADMIN"}, nil, "https://malicious.example")
	if w.Code != 403 {
		t.Fatal("CSRF permitido")
	}
	c := demoLogin(t, s, "ADMIN")
	if w = request(t, s, "GET", "/me", nil, c, ""); w.Code != 200 {
		t.Fatal(w.Body)
	}
	if w = request(t, s, "POST", "/auth/logout", map[string]any{}, c, s.cfg.Origin); w.Code != 200 {
		t.Fatal(w.Body)
	}
	if w = request(t, s, "GET", "/me", nil, c, ""); w.Code != 401 {
		t.Fatal("sesión no revocada")
	}
	var raw string
	_ = s.store.db.QueryRow("SELECT token_hash FROM sessions LIMIT 1").Scan(&raw)
	if strings.Contains(raw, c.Value) {
		t.Fatal("token almacenado sin hash")
	}
}
func TestRealRegistrationLoginPersistenceAndNoDemoBackdoor(t *testing.T) {
	s := testServer(t, false)
	payload := map[string]string{"name": "María", "phone": "999123456", "email": "maria@example.com", "password": "UnBuenDia2026!"}
	w := request(t, s, "POST", "/auth/signup", payload, nil, s.cfg.Origin)
	if w.Code != 200 {
		t.Fatalf("registro: %d %s", w.Code, w.Body)
	}
	c := w.Result().Cookies()[0]
	var hash, role string
	_ = s.store.db.QueryRow("SELECT password_hash,role FROM users WHERE email=?", payload["email"]).Scan(&hash, &role)
	if role != "CLIENT" || strings.Contains(hash, payload["password"]) || !checkPassword(hash, payload["password"]) {
		t.Fatal("hash o rol incorrecto")
	}
	w = request(t, s, "POST", "/auth/login", map[string]any{"email": payload["email"], "password": "incorrecta"}, nil, s.cfg.Origin)
	if w.Code != 401 {
		t.Fatal("contraseña incorrecta aceptada")
	}
	w = request(t, s, "POST", "/auth/login", map[string]any{"email": payload["email"], "password": payload["password"]}, nil, s.cfg.Origin)
	if w.Code != 200 {
		t.Fatal(w.Body)
	}
	w = request(t, s, "POST", "/auth/demo", map[string]string{"role": "ADMIN"}, nil, s.cfg.Origin)
	if w.Code != 404 {
		t.Fatal("acceso demo expuesto en modo sqlite")
	}
	cfg := s.cfg
	if e := s.store.db.Close(); e != nil {
		t.Fatal(e)
	}
	reopened, e := newServer(cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.store.db.Close()
	w = request(t, reopened, "GET", "/me", nil, c, "")
	if w.Code != 200 {
		t.Fatal("sesión no sobrevivió al reinicio", w.Body)
	}
}
func TestCashClosureBlocksNewPaymentsButAllowsOtherWork(t *testing.T) {
	s := testServer(t, true)
	admin := demoLogin(t, s, "ADMIN")
	report := buildReport(snapshot(t, s, "ADMIN").Orders, today())
	w := request(t, s, "POST", "/cash-closures", map[string]any{"date": today(), "expected_total": Money(report.Revenue + 1)}, admin, s.cfg.Origin)
	if w.Code != 409 {
		t.Fatal("cierre desactualizado aceptado", w.Body)
	}
	w = request(t, s, "POST", "/cash-closures", map[string]any{"date": today(), "expected_total": report.Revenue}, admin, s.cfg.Origin)
	if w.Code != 200 {
		t.Fatal(w.Body)
	}
	o, _ := newOrder(t, s)
	if _, e := s.store.orderAction(demoUser("ADMIN"), o.ID, "payment", "", "", "CASH", ""); e == nil {
		t.Fatal("caja cerrada aceptó cobro")
	}
	p := snapshot(t, s, "ADMIN").Menu[0]
	p.Name += " actualizado"
	w = request(t, s, "PUT", "/menu", p, admin, s.cfg.Origin)
	if w.Code != 200 {
		t.Fatal("cierre bloqueó edición no relacionada", w.Body)
	}
}
func TestOptimisticStockEditRejectsStaleVersion(t *testing.T) {
	s := testServer(t, true)
	admin := demoLogin(t, s, "ADMIN")
	p := snapshot(t, s, "ADMIN").Menu[0]
	stale := p
	p.Stock++
	if w := request(t, s, "PUT", "/menu", p, admin, s.cfg.Origin); w.Code != 200 {
		t.Fatal(w.Body)
	}
	if w := request(t, s, "PUT", "/menu", stale, admin, s.cfg.Origin); w.Code != 409 {
		t.Fatal("edición obsoleta aceptada", w.Body)
	}
}
func TestMoneyReportsAndCSV(t *testing.T) {
	var m Money
	for _, invalid := range []string{"-1", "1.001", "null", "1e30"} {
		if json.Unmarshal([]byte(invalid), &m) == nil {
			t.Error("importe aceptado", invalid)
		}
	}
	if e := json.Unmarshal([]byte("0.10"), &m); e != nil || m != 10 {
		t.Fatal("pérdida de precisión")
	}
	date := "2026-11-04"
	o := Order{ID: randomID(), Name: "=CMD()", Total: 30, Status: "DELIVERED", Created: "2026-11-05T02:00:00Z", Updated: "2026-11-05T02:00:00Z", Items: []Line{{ProductID: "a", Name: "+nombre", Price: 10, Quantity: 3}}, Payment: &Payment{"YAPE", 30, "ref", "2026-11-05T02:00:00Z"}}
	r := buildReport([]Order{o}, date)
	if r.Revenue != 30 || r.PaidUnits != 3 || r.PaidOrders != 1 {
		t.Fatal("jornada o sumas incorrectas")
	}
	var b bytes.Buffer
	writeCSV(&b, r)
	if !strings.Contains(b.String(), "'=CMD()") || !strings.Contains(b.String(), "'+nombre") {
		t.Fatal("CSV permite fórmulas")
	}
}
func TestBackupAndReopen(t *testing.T) {
	s := testServer(t, true)
	o, _ := newOrder(t, s)
	path := filepath.Join(t.TempDir(), "backup.db")
	if e := s.store.backup(path); e != nil {
		t.Fatal(e)
	}
	store, e := openStore(path, false)
	if e != nil {
		t.Fatal(e)
	}
	defer store.db.Close()
	var id string
	if e = store.db.QueryRow("SELECT id FROM orders WHERE id=?", o.ID).Scan(&id); e != nil || id != o.ID {
		t.Fatal("backup incompleto")
	}
}
func TestRateLimit(t *testing.T) {
	s := testServer(t, false)
	for i := 0; i < 13; i++ {
		w := request(t, s, "POST", "/auth/demo", map[string]string{"role": "ADMIN"}, nil, s.cfg.Origin)
		if i == 12 && w.Code != 429 {
			t.Fatal("límite de intentos no aplicado")
		}
	}
}
func TestInvalidAndExpiredSessions(t *testing.T) {
	s := testServer(t, true)
	c := demoLogin(t, s, "CLIENT")
	_, _ = s.store.db.Exec("UPDATE sessions SET expires_at=?", time.Now().Add(-time.Hour).Unix())
	if w := request(t, s, "GET", "/me", nil, c, ""); w.Code != 401 {
		t.Fatal("sesión vencida aceptada")
	}
}
func TestTwoStoresCompeteForLastUnit(t *testing.T) {
	s := testServer(t, true)
	second, e := openStore(s.cfg.DBPath, false)
	if e != nil {
		t.Fatal(e)
	}
	defer second.db.Close()
	p := snapshot(t, s, "ADMIN").Menu[0]
	_, e = s.store.mutateAs(demoUser("ADMIN").ID, func(st *BusinessState) (any, error) {
		for i := range st.Menu {
			if st.Menu[i].ID == p.ID {
				st.Menu[i].Stock = 1
			}
		}
		return nil, nil
	})
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, store := range []*Store{s.store, second} {
		wg.Add(1)
		go func(d *Store) {
			defer wg.Done()
			_, err := d.create(demoUser("CLIENT"), Checkout{randomID(), []Line{{ProductID: p.ID, Quantity: 1}}})
			results <- err
		}(store)
	}
	wg.Wait()
	close(results)
	wins := 0
	for err := range results {
		if err == nil {
			wins++
		}
	}
	if wins != 1 {
		t.Fatalf("dos conexiones vendieron %d veces la última unidad", wins)
	}
}
func TestAuditRecordsCancellationReason(t *testing.T) {
	s := testServer(t, true)
	o, _ := newOrder(t, s)
	_, e := s.store.orderAction(demoUser("CLIENT"), o.ID, "status", "CANCELLED", "Motivo auditado", "", "")
	if e != nil {
		t.Fatal(e)
	}
	var reason string
	if e = s.store.db.QueryRow("SELECT reason FROM order_status_history WHERE order_id=? AND new_status='CANCELLED'", o.ID).Scan(&reason); e != nil || reason != "Motivo auditado" {
		t.Fatal("motivo no persistido", reason, e)
	}
}
func TestRoleChangeInvalidatesPrivilegeImmediately(t *testing.T) {
	s := testServer(t, true)
	cook := demoLogin(t, s, "COOK")
	admin := demoLogin(t, s, "ADMIN")
	w := request(t, s, "PATCH", "/users/"+demoUser("COOK").ID, map[string]string{"role": "CLIENT"}, admin, s.cfg.Origin)
	if w.Code != 200 {
		t.Fatal(w.Body)
	}
	w = request(t, s, "GET", "/reports/daily?date="+today(), nil, cook, "")
	if w.Code != 403 {
		t.Fatal("sesión conservó rol retirado")
	}
	w = request(t, s, "PATCH", "/users/"+demoUser("ADMIN").ID, map[string]string{"role": "CLIENT"}, admin, s.cfg.Origin)
	if w.Code != 400 {
		t.Fatal("administrador pudo quitarse sus permisos")
	}
}
func TestUploadRejectsExecutableAndClient(t *testing.T) {
	s := testServer(t, true)
	admin := demoLogin(t, s, "ADMIN")
	client := demoLogin(t, s, "CLIENT")
	w := request(t, s, "POST", "/images", nil, client, s.cfg.Origin)
	if w.Code != 403 {
		t.Fatal("cliente pudo intentar subir")
	}
	w = request(t, s, "POST", "/images", map[string]string{"file": "not-image"}, admin, s.cfg.Origin)
	if w.Code != 400 {
		t.Fatal("formato de subida inválido aceptado")
	}
}
func TestFictionalCharactersLoginByUsernameAndEmail(t *testing.T) {
	s := testServer(t, false)
	count, e := s.store.seedTestCharacters()
	if e != nil || count != 4 {
		t.Fatalf("seed: %d %v", count, e)
	}
	for _, character := range testCharacters() {
		for _, identifier := range []string{character.User.Email, strings.Split(character.User.Email, "@")[0]} {
			w := request(t, s, "POST", "/auth/login", map[string]string{"email": identifier, "password": character.Password, "access": character.User.Role}, nil, s.cfg.Origin)
			if w.Code != 200 {
				t.Fatalf("login %s: %d %s", identifier, w.Code, w.Body)
			}
			var u User
			if e = json.Unmarshal(w.Body.Bytes(), &u); e != nil || u.ID != character.User.ID || u.Role != character.User.Role {
				t.Fatal("identidad o rol incorrectos")
			}
			cookie := w.Result().Cookies()[0]
			me := request(t, s, "GET", "/me", nil, cookie, "")
			if me.Code != 200 {
				t.Fatal("sesión no válida")
			}
		}
	}
	w := request(t, s, "POST", "/auth/login", map[string]string{"email": "cliente", "password": "Valentina2026!", "access": "ADMIN"}, nil, s.cfg.Origin)
	if w.Code != 403 || len(w.Result().Cookies()) != 0 {
		t.Fatal("cliente pudo entrar por acceso admin")
	}
	w = request(t, s, "POST", "/auth/login", map[string]string{"email": "admin", "password": "incorrecta", "access": "ADMIN"}, nil, s.cfg.Origin)
	if w.Code != 401 {
		t.Fatal("contraseña ficticia incorrecta aceptada")
	}
}
func TestFictionalSeedPreservesChangedPassword(t *testing.T) {
	s := testServer(t, true)
	if _, e := s.store.seedTestCharacters(); e != nil {
		t.Fatal(e)
	}
	changed, e := hashPassword("PasswordCambiado2026!")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.store.db.Exec("UPDATE users SET password_hash=? WHERE email=?", changed, "cliente@demo.local"); e != nil {
		t.Fatal(e)
	}
	n, e := s.store.seedTestCharacters()
	if e != nil || n != 0 {
		t.Fatalf("repetir seed: %d %v", n, e)
	}
	var actual string
	_ = s.store.db.QueryRow("SELECT password_hash FROM users WHERE email=?", "cliente@demo.local").Scan(&actual)
	if actual != changed {
		t.Fatal("seed restableció contraseña modificada")
	}
}
func TestFictionalSeedDoesNotElevateUnrelatedAccount(t *testing.T) {
	s := testServer(t, false)
	u, e := s.store.createUser("Otra persona", "999999999", "admin@demo.local", "UnaCuentaExistente2026!", "CLIENT")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.store.seedTestCharacters(); e == nil {
		t.Fatal("se aceptó conflicto de identidad")
	}
	var role string
	var count int
	_ = s.store.db.QueryRow("SELECT role FROM users WHERE id=?", u.ID).Scan(&role)
	_ = s.store.db.QueryRow("SELECT count(*) FROM users").Scan(&count)
	if role != "CLIENT" || count != 1 {
		t.Fatal("seed elevó permisos o confirmó un alta parcial")
	}
}
func TestCookBreakfastCRUDAndHistoricalOrders(t *testing.T) {
	s := testServer(t, true)
	cook := demoLogin(t, s, "COOK")
	client := demoLogin(t, s, "CLIENT")
	p := Product{ID: randomID(), Name: "Desayuno de prueba CRUD", Description: "Pan y bebida caliente", Price: 750, Category: "COMBO", Stock: 8, Available: true}
	w := request(t, s, "PUT", "/menu", p, cook, s.cfg.Origin)
	if w.Code != 200 {
		t.Fatalf("crear %d %s", w.Code, w.Body)
	}
	if e := json.Unmarshal(w.Body.Bytes(), &p); e != nil {
		t.Fatal(e)
	}
	p.Description = "Receta actualizada por cocina"
	p.Price = 800
	w = request(t, s, "PUT", "/menu", p, cook, s.cfg.Origin)
	if w.Code != 200 {
		t.Fatal("editar", w.Body)
	}
	_ = json.Unmarshal(w.Body.Bytes(), &p)
	checkout := Checkout{randomID(), []Line{{ProductID: p.ID, Quantity: 2}}}
	v, e := s.store.create(demoUser("CLIENT"), checkout)
	if e != nil {
		t.Fatal(e)
	}
	order := v.(Order)
	for _, item := range snapshot(t, s, "COOK").Menu {
		if item.ID == p.ID {
			p = item
		}
	}
	w = request(t, s, "DELETE", "/menu/"+p.ID, map[string]string{"expected_updated_at": p.Updated}, cook, s.cfg.Origin)
	if w.Code != 200 {
		t.Fatal("eliminar", w.Body)
	}
	staff := snapshot(t, s, "COOK")
	var archived Product
	for _, item := range staff.Menu {
		if item.ID == p.ID {
			t.Fatal("eliminado permanece en carta")
		}
	}
	for _, item := range staff.Archived {
		if item.ID == p.ID {
			archived = item
		}
	}
	if archived.ID == "" {
		t.Fatal("falta en eliminados")
	}
	visible := snapshot(t, s, "CLIENT")
	if len(visible.Archived) != 0 {
		t.Fatal("cliente lee eliminados")
	}
	for _, item := range visible.Menu {
		if item.ID == p.ID {
			t.Fatal("cliente ve eliminado")
		}
	}
	w = request(t, s, "POST", "/orders", Checkout{randomID(), []Line{{ProductID: p.ID, Quantity: 1}}}, client, s.cfg.Origin)
	if w.Code != 400 {
		t.Fatal("cliente compró eliminado", w.Body)
	}
	w = request(t, s, "POST", "/orders/"+order.ID+"/status", map[string]string{"status": "CANCELLED", "reason": "Prueba de cancelación con producto archivado"}, client, s.cfg.Origin)
	if w.Code != 200 {
		t.Fatal("cancelar histórico", w.Body)
	}
	for _, item := range snapshot(t, s, "COOK").Archived {
		if item.ID == p.ID {
			archived = item
		}
	}
	if archived.Stock != 8 {
		t.Fatal("no devolvió stock de producto eliminado", archived.Stock)
	}
	w = request(t, s, "POST", "/menu/"+p.ID+"/restore", map[string]string{"expected_updated_at": archived.Updated}, cook, s.cfg.Origin)
	if w.Code != 200 {
		t.Fatal("restaurar", w.Body)
	}
	found := false
	for _, item := range snapshot(t, s, "CLIENT").Menu {
		if item.ID == p.ID {
			found = item.Available && item.Stock == 8 && item.Price == 800
		}
	}
	if !found {
		t.Fatal("producto restaurado incorrecto")
	}
	var histories int
	_ = s.store.db.QueryRow("SELECT count(*) FROM menu_item_history WHERE menu_item_id=?", p.ID).Scan(&histories)
	if histories != 2 {
		t.Fatal("auditoría incompleta")
	}
	var itemCount int
	_ = s.store.db.QueryRow("SELECT count(*) FROM order_items WHERE order_id=?", order.ID).Scan(&itemCount)
	if itemCount != 1 {
		t.Fatal("se perdió el detalle histórico")
	}
}
func TestBreakfastDeletionPermissionsAndStaleVersion(t *testing.T) {
	s := testServer(t, true)
	client := demoLogin(t, s, "CLIENT")
	cook := demoLogin(t, s, "COOK")
	p := snapshot(t, s, "COOK").Menu[0]
	for _, action := range []struct{ Method, Path string }{{"DELETE", "/menu/" + p.ID}, {"POST", "/menu/" + p.ID + "/restore"}} {
		w := request(t, s, action.Method, action.Path, map[string]string{"expected_updated_at": p.Updated}, client, s.cfg.Origin)
		if w.Code != 403 {
			t.Fatal("cliente obtuvo acceso de gestión", w.Body)
		}
	}
	w := request(t, s, "DELETE", "/menu/"+p.ID, map[string]string{"expected_updated_at": "obsoleto"}, cook, s.cfg.Origin)
	if w.Code != 409 {
		t.Fatal("borrado con versión obsoleta aceptado", w.Body)
	}
	w = request(t, s, "DELETE", "/menu/"+randomID(), map[string]string{"expected_updated_at": p.Updated}, cook, s.cfg.Origin)
	if w.Code != 404 {
		t.Fatal("borrado de inexistente incorrecto", w.Body)
	}
}
func TestArchivedBreakfastCannotBeOverwrittenAndDeletionIsIdempotent(t *testing.T) {
	s := testServer(t, true)
	cook := demoLogin(t, s, "COOK")
	p := snapshot(t, s, "COOK").Menu[0]
	for i := 0; i < 2; i++ {
		w := request(t, s, "DELETE", "/menu/"+p.ID, map[string]string{"expected_updated_at": p.Updated}, cook, s.cfg.Origin)
		if w.Code != 200 {
			t.Fatal(w.Body)
		}
	}
	for _, a := range snapshot(t, s, "COOK").Archived {
		if a.ID == p.ID {
			p = a
		}
	}
	p.Available = true
	w := request(t, s, "PUT", "/menu", p, cook, s.cfg.Origin)
	if w.Code != 409 {
		t.Fatal("edición restauró implícitamente producto eliminado", w.Body)
	}
	var n int
	_ = s.store.db.QueryRow("SELECT count(*) FROM menu_item_history WHERE menu_item_id=? AND action='ARCHIVE'", p.ID).Scan(&n)
	if n != 1 {
		t.Fatal("eliminación duplicó efectos")
	}
}
