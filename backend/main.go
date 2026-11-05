package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func main() {
	c, e := loadConfig()
	if e != nil {
		log.Fatal(e)
	}
	s, e := newServer(c)
	if e != nil {
		log.Fatal(e)
	}
	defer s.store.db.Close()
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "seed-users":
			count, err := s.store.seedTestCharacters()
			if err != nil {
				log.Fatal(err)
			}
			fmt.Printf("Personajes ficticios creados o habilitados: %d. Cuentas existentes conservadas.\n", count)
			return
		case "user":
			flags := flag.NewFlagSet("user", flag.ExitOnError)
			email := flags.String("email", "", "Correo")
			name := flags.String("name", "", "Nombre")
			phone := flags.String("phone", "", "Teléfono")
			role := flags.String("role", "ADMIN", "ADMIN, COOK o CLIENT")
			_ = flags.Parse(os.Args[2:])
			password := os.Getenv("ADMIN_PASSWORD")
			if password == "" {
				log.Fatal("Indique la contraseña mediante ADMIN_PASSWORD, nunca en argumentos")
			}
			u, e := s.store.createUser(*name, *phone, *email, password, *role)
			if e != nil {
				log.Fatal(e)
			}
			fmt.Printf("Usuario creado: %s · %s\n", u.Email, u.Role)
			return
		case "check":
			if e = s.store.integrity(); e != nil {
				log.Fatal(e)
			}
			var version string
			_ = s.store.db.QueryRow("SELECT sqlite_version()").Scan(&version)
			fmt.Printf("SQLite %s · integrity_check=ok · %s\n", version, c.DBPath)
			return
		case "backup":
			path := filepath.Join(c.Data, "backup-"+time.Now().Format("20060102-150405")+".db")
			if e = s.store.backup(path); e != nil {
				log.Fatal(e)
			}
			fmt.Println("Respaldo consistente:", path)
			return
		default:
			log.Fatal("Comando desconocido: user, seed-users, check o backup")
		}
	}
	server := &http.Server{Addr: c.Addr, Handler: s.routes(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	log.Printf("Desayunos API · %s · SQLite · http://%s", c.Mode, c.Addr)
	if e = server.ListenAndServe(); e != nil && e != http.ErrServerClosed {
		log.Fatal(e)
	}
}
