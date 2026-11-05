package main

import "database/sql"

func canManageMenu(u User) bool { return u.Role == "ADMIN" || u.Role == "COOK" }

// Archivar mantiene las referencias de pedidos y permite restauración desde la UI.
func (s *Store) archiveProduct(u User, id, expected string, restore bool) (any, error) {
	if !canManageMenu(u) {
		return nil, denied()
	}
	if !validID(id) || expected == "" {
		return nil, bad("Producto y versión requeridos")
	}
	tx, e := s.db.Begin()
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	var updated string
	var available bool
	e = tx.QueryRow("SELECT updated_at,is_available FROM menu_items WHERE id=?", id).Scan(&updated, &available)
	if e == sql.ErrNoRows {
		return nil, &APIError{404, "Desayuno no encontrado"}
	}
	if e != nil {
		return nil, e
	}
	var wasAvailable bool
	e = tx.QueryRow("SELECT was_available FROM menu_item_archives WHERE menu_item_id=?", id).Scan(&wasAvailable)
	archived := e == nil
	if e != nil && e != sql.ErrNoRows {
		return nil, e
	}
	if (!restore && archived) || (restore && !archived) {
		return map[string]bool{"ok": true}, nil
	}
	if updated != expected {
		return nil, &APIError{409, "El desayuno o su stock cambió. Actualiza antes de continuar."}
	}
	action := "ARCHIVE"
	stamp := now()
	if restore {
		action = "RESTORE"
		_, e = tx.Exec("DELETE FROM menu_item_archives WHERE menu_item_id=?", id)
		available = wasAvailable
	} else {
		_, e = tx.Exec("INSERT INTO menu_item_archives VALUES(?,?,?,?)", id, u.ID, stamp, available)
		available = false
	}
	if e != nil {
		return nil, e
	}
	if _, e = tx.Exec("UPDATE menu_items SET is_available=?,updated_at=? WHERE id=?", available, stamp, id); e != nil {
		return nil, e
	}
	if _, e = tx.Exec("INSERT INTO menu_item_history(menu_item_id,action,actor_id,created_at) VALUES(?,?,?,?)", id, action, u.ID, stamp); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return map[string]bool{"ok": true}, nil
}
