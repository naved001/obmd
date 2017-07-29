package main

import (
	"database/sql"
)

func initDB(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS nodes (
		id INTEGER PRIMARY KEY,
		label VARCHAR(80) NOT NULL UNIQUE,
		ipmi_user VARCHAR(80) NOT NULL,
		ipmi_pass VARCHAR(80) NOT NULL,
		ipmi_addr VARCHAR(80) NOT NULL,
		owner VARCHAR(80)
	)`)
	return err
}
