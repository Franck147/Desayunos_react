package main

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string { return e.Message }
func bad(message string) error    { return &APIError{400, message} }
func denied() error               { return &APIError{403, "No tiene permisos para esta operación"} }
func randomID() string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	h := hex.EncodeToString(b)
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}
func validID(s string) bool {
	if len(s) != 36 {
		return false
	}
	_, e := hex.DecodeString(strings.ReplaceAll(s, "-", ""))
	return e == nil && s[8] == '-' && s[13] == '-' && s[18] == '-' && s[23] == '-'
}

type Config struct {
	Mode, Origin, Addr, Data, DBPath string
	Secure                           bool
}

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func loadConfig() (Config, error) {
	c := Config{Mode: env("APP_MODE", "sqlite"), Origin: env("APP_ORIGIN", "http://127.0.0.1:3000"), Addr: env("HTTP_ADDR", "127.0.0.1:"+env("PORT", "8080")), Data: env("DATA_DIR", "../.data"), Secure: os.Getenv("COOKIE_SECURE") == "true"}
	if c.Mode != "sqlite" && c.Mode != "demo" {
		return c, errors.New("APP_MODE debe ser sqlite o demo")
	}
	u, e := url.Parse(c.Origin)
	if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.Path != "" {
		return c, errors.New("APP_ORIGIN debe ser un origen HTTP/HTTPS sin ruta")
	}
	if u.Scheme == "https" && !c.Secure {
		return c, errors.New("Active COOKIE_SECURE=true para HTTPS")
	}
	name := "desayunos.db"
	if c.Mode == "demo" {
		name = "demo.db"
	}
	c.DBPath = env("DATABASE_PATH", filepath.Join(c.Data, name))
	if e = os.MkdirAll(c.Data, 0700); e != nil {
		return c, e
	}
	return c, nil
}

type Session struct{ User User }
type attempt struct {
	Count int
	Until time.Time
}
type Server struct {
	cfg      Config
	store    *Store
	mu       sync.Mutex
	attempts map[string]attempt
}

func newServer(c Config) (*Server, error) {
	store, e := openStore(c.DBPath, c.Mode == "demo")
	if e != nil {
		return nil, e
	}
	if e = store.integrity(); e != nil {
		store.db.Close()
		return nil, e
	}
	store.cleanupSessions()
	return &Server{cfg: c, store: store, attempts: map[string]attempt{}}, nil
}
func hashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, e := rand.Read(salt); e != nil {
		return "", e
	}
	key, e := pbkdf2.Key(sha256.New, password, salt, 600000, 32)
	if e != nil {
		return "", e
	}
	return "pbkdf2-sha256$600000$" + base64.StdEncoding.EncodeToString(salt) + "$" + base64.StdEncoding.EncodeToString(key), nil
}
func checkPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" || parts[1] != "600000" {
		return false
	}
	salt, e := base64.StdEncoding.DecodeString(parts[2])
	if e != nil {
		return false
	}
	expected, e := base64.StdEncoding.DecodeString(parts[3])
	if e != nil {
		return false
	}
	key, e := pbkdf2.Key(sha256.New, password, salt, 600000, 32)
	return e == nil && subtle.ConstantTimeCompare(key, expected) == 1
}
func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
func (s *Server) createSession(w http.ResponseWriter, r *http.Request, u User) error {
	token := randomID() + randomID()
	until := time.Now().Add(7 * 24 * time.Hour)
	tx, e := s.store.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if c, e := r.Cookie("desayunos_session"); e == nil {
		if _, e = tx.Exec("DELETE FROM sessions WHERE token_hash=?", tokenHash(c.Value)); e != nil {
			return e
		}
	}
	if _, e = tx.Exec("INSERT INTO sessions VALUES(?,?,?)", tokenHash(token), u.ID, until.Unix()); e != nil {
		return e
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	http.SetCookie(w, &http.Cookie{Name: "desayunos_session", Value: token, Path: "/", HttpOnly: true, Secure: s.cfg.Secure, SameSite: http.SameSiteLaxMode, Expires: until, MaxAge: 7 * 86400})
	return nil
}
func (s *Server) session(r *http.Request) (Session, error) {
	var v Session
	c, e := r.Cookie("desayunos_session")
	if e != nil {
		return v, &APIError{401, "Inicie sesión para continuar"}
	}
	e = s.store.db.QueryRow(`SELECT u.id,u.full_name,u.phone,u.email,u.role FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=? AND s.expires_at>?`, tokenHash(c.Value), time.Now().Unix()).Scan(&v.User.ID, &v.User.Name, &v.User.Phone, &v.User.Email, &v.User.Role)
	if errors.Is(e, sql.ErrNoRows) {
		return v, &APIError{401, "Su sesión expiró. Inicie sesión nuevamente."}
	}
	return v, e
}
func (s *Server) allowAuth(r *http.Request) bool {
	ip, _, e := net.SplitHostPort(r.RemoteAddr)
	if e != nil {
		ip = r.RemoteAddr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range s.attempts {
		if time.Now().After(v.Until) {
			delete(s.attempts, k)
		}
	}
	a := s.attempts[ip]
	if a.Until.IsZero() {
		a.Until = time.Now().Add(time.Minute)
	}
	a.Count++
	s.attempts[ip] = a
	return a.Count <= 12
}
func validEmail(email string) bool {
	addr, e := mail.ParseAddress(email)
	return e == nil && addr.Address == email && len(email) <= 254
}
func (s *Store) createUser(name, phone, email, password, role string) (User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	name = strings.TrimSpace(name)
	phone = strings.TrimSpace(phone)
	if !validEmail(email) || name == "" || len(name) > 150 || phone == "" || len(phone) > 40 || len(password) < 8 || len(password) > 128 {
		return User{}, bad("Revise sus datos. La contraseña debe tener entre 8 y 128 caracteres.")
	}
	if role != "CLIENT" && role != "ADMIN" && role != "COOK" {
		return User{}, bad("Rol inválido")
	}
	encoded, e := hashPassword(password)
	if e != nil {
		return User{}, e
	}
	u := User{randomID(), name, phone, email, role}
	_, e = s.db.Exec("INSERT INTO users VALUES(?,?,?,?,?,?,?)", u.ID, u.Email, u.Name, u.Phone, u.Role, encoded, now())
	if e != nil {
		if strings.Contains(e.Error(), "UNIQUE constraint failed: users.email") {
			return User{}, &APIError{409, "Ya existe una cuenta con este correo"}
		}
		return User{}, e
	}
	return u, nil
}
func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		if e := s.store.integrity(); e != nil {
			writeError(w, e)
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "mode": s.cfg.Mode, "database": "sqlite"})
	})
	mux.HandleFunc("GET /api/v1/media/{name}", s.media)
	mux.HandleFunc("/api/v1/", s.handle)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Method != "GET" && r.Method != "HEAD" {
			if r.Header.Get("Origin") != s.cfg.Origin || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				writeError(w, denied())
				return
			}
		}
		r.Body = http.MaxBytesReader(w, r.Body, 6<<20)
		defer func() {
			if v := recover(); v != nil {
				log.Printf("Error interno en %s", r.URL.Path)
				writeError(w, errors.New("error interno"))
			}
		}()
		mux.ServeHTTP(w, r)
	})
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, e error) {
	var ae *APIError
	if errors.As(e, &ae) {
		writeJSON(w, ae.Status, map[string]string{"error": ae.Message})
		return
	}
	log.Printf("Error interno: %v", e)
	writeJSON(w, 500, map[string]string{"error": "No se pudo completar la operación"})
}
func decode(r *http.Request, v any) error {
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return bad("Datos inválidos o campos no permitidos")
	}
	var rest any
	if d.Decode(&rest) != io.EOF {
		return bad("Cuerpo de solicitud inválido")
	}
	return nil
}
func (s *Server) media(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	stem, ext, ok := strings.Cut(name, ".")
	if !ok || !validID(stem) || (ext != "png" && ext != "jpg" && ext != "webp") {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(w, r, filepath.Join(s.cfg.Data, "uploads", name))
}
func (s *Server) upload(r *http.Request) (any, error) {
	if e := r.ParseMultipartForm(6 << 20); e != nil {
		return nil, bad("No se pudo leer la imagen (máximo 5 MB)")
	}
	defer r.MultipartForm.RemoveAll()
	f, _, e := r.FormFile("file")
	if e != nil {
		return nil, bad("Seleccione una imagen")
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, (5<<20)+1))
	if e != nil || len(b) > 5<<20 {
		return nil, bad("La imagen supera 5 MB")
	}
	mime := http.DetectContentType(b)
	ext := map[string]string{"image/png": "png", "image/jpeg": "jpg", "image/webp": "webp"}[mime]
	if ext == "" {
		return nil, bad("Use una imagen JPG, PNG o WebP válida")
	}
	dir := filepath.Join(s.cfg.Data, "uploads")
	if e = os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	name := randomID() + "." + ext
	if e = os.WriteFile(filepath.Join(dir, name), b, 0600); e != nil {
		return nil, e
	}
	return map[string]string{"url": "/api/v1/media/" + name}, nil
}
