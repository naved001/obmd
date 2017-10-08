package main

import (
	"database/sql"

	"github.com/zenhack/obmd/internal/driver"
)

// Persistent store for node info, + ephemeral tracking of live OBM
// connections.
//
// This is basically a map[string]*Node, except that it (a) persists changes in
// metadata to a database, (b) will shutdown/initialize OBMs as needed, and (c)
// will update the node version number when changes are made.
//
// Note that this is not thread-safe.
type State struct {
	db     *sql.DB
	nodes  map[string]*Node
	driver driver.Driver
}

// Create a State from a database. This operates lazily, loading objects when
// they are first needed.
func NewState(db *sql.DB, driver driver.Driver) (*State, error) {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS nodes (
		label VARCHAR(80) PRIMARY KEY,
		obm_info TEXT NOT NULL,
		version BIGINT NOT NULL
	)`)
	if err != nil {
		return nil, err
	}
	err = db.Ping()
	if err != nil {
		return nil, err
	}
	return &State{
		nodes:  make(map[string]*Node),
		db:     db,
		driver: driver,
	}, nil
}

func (s *State) GetNode(label string) (*Node, error) {
	return s.getNode(s.db, label)
}

// Some methods common to sql.DB and sql.Tx. Feel free to extend as needed.
type sqlSession interface {
	QueryRow(query string, args ...interface{}) *sql.Row
}

// version of GetNode that uses a sqlSession, rather than acting directly
// on s.db.
func (s *State) getNode(sess sqlSession, label string) (*Node, error) {
	node, ok := s.nodes[label]
	if ok {
		return node, nil
	}
	obmInfo := []byte{}
	version := uint64(0)
	err := sess.QueryRow(
		`SELECT obm_info, version
		FROM nodes
		WHERE label = ?`,
		label,
	).Scan(&version, &obmInfo)
	if err != nil {
		return nil, err
	}
	node, err = NewNode(label, s.driver, obmInfo)
	if err != nil {
		s.nodes[label] = node
	}
	return node, nil
}

func (s *State) SetNode(label string, info []byte) (*Node, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	node, err := s.getNode(tx, label)
	switch err {
	case sql.ErrNoRows:
		// Node doesn't exist; create it.
		node, err := NewNode(label, s.driver, info)
		if err != nil {
			return nil, err
		}
		_, err = tx.Exec(
			`INSERT INTO nodes(label, obm_info, version)
			VALUES (?, ?, ?)`,
			label,
			info,
			0,
		)
		if err != nil {
			node.ObmCancel()
			tx.Rollback()
			return nil, err
		}
		err = tx.Commit()
		if err != nil {
			node.ObmCancel()
			return nil, err
		}
		s.nodes[label] = node
		return node, nil
	case nil:
		// Node already exists; update the info and bump the version.
		newVersion := node.Version + 1
		_, err := tx.Exec(
			`UPDATE nodes
			SET (version, obm_info) = (?, ?)
			WHERE label = ?`,
			newVersion, info, label,
		)
		if err != nil {
			tx.Rollback()
			return nil, err
		}
		newNode, err := NewNode(label, s.driver, info)
		if err != nil {
			tx.Rollback()
			return nil, err
		}
		newNode.Version = newVersion
		err = tx.Commit()
		if err != nil {
			newNode.ObmCancel()
			return nil, err
		}
		node.ObmCancel()
		s.nodes[label] = newNode
		return newNode, nil
	default:
		tx.Rollback()
		return nil, err
	}
}

func (s *State) DeleteNode(label string) error {
	node, ok := s.nodes[label]
	if ok {
		node.ObmCancel()
		delete(s.nodes, label)
	}
	_, err := s.db.Exec("DELETE FROM nodes WHERE label = ?", label)
	return err
}
