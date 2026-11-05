package main

import (
	"database/sql"
	"errors"
	"fmt"
)

type TestCharacter struct {
	User     User
	Password string
}

func testCharacters() []TestCharacter {
	return []TestCharacter{
		{demoUser("CLIENT"), "Valentina2026!"},
		{User{"10000000-0000-4000-8000-000000000004", "Diego Mendoza", "999111004", "diego@demo.local", "CLIENT"}, "Diego2026!"},
		{demoUser("COOK"), "Marco2026!"},
		{demoUser("ADMIN"), "Adriana2026!"},
	}
}

// Alta explícita de personajes de prueba. No cambia contraseñas ni roles existentes.
// Solo habilita las cuentas demo conocidas cuyo password sigue desactivado.
func (s *Store) seedTestCharacters() (int, error) {
	tx, e := s.db.Begin()
	if e != nil {
		return 0, e
	}
	defer tx.Rollback()
	created := 0
	for _, character := range testCharacters() {
		u := character.User
		var id, hash, role string
		e = tx.QueryRow("SELECT id,password_hash,role FROM users WHERE email=? COLLATE NOCASE", u.Email).Scan(&id, &hash, &role)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return 0, e
		}
		if e == nil {
			if id != u.ID || role != u.Role {
				return 0, fmt.Errorf("El correo %s pertenece a otra cuenta o tiene otro rol; no se modificó", u.Email)
			}
			if hash != "!demo-disabled-password" {
				continue
			}
			encoded, err := hashPassword(character.Password)
			if err != nil {
				return 0, err
			}
			if _, err = tx.Exec("UPDATE users SET password_hash=? WHERE id=? AND password_hash=?", encoded, u.ID, "!demo-disabled-password"); err != nil {
				return 0, err
			}
			created++
			continue
		}
		encoded, err := hashPassword(character.Password)
		if err != nil {
			return 0, err
		}
		if _, err = tx.Exec("INSERT INTO users VALUES(?,?,?,?,?,?,?)", u.ID, u.Email, u.Name, u.Phone, u.Role, encoded, now()); err != nil {
			return 0, err
		}
		created++
	}
	return created, tx.Commit()
}
