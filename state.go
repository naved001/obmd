package main

import (
	"database/sql"
	"sync"
)

// Global state; used to look up nodes/console tokens.
type State struct {
	sync.Mutex
	db *sql.DB

	// `nodes` serves two purposes:
	//
	// 1. A cache for the values in the database
	// 2. A place to store active tokens and connections.
	//
	// (2) is mandatory, so we may as well keep the ipmi info in memory
	// too. Otherwise, it might be nice to avoid duplicating the state,
	// if only for simplicity.
	nodes map[string]*Node
}

// Create a State from a database. This operates lazily, loading objects when
// they are first needed.
func NewState(db *sql.DB) (*State, error) {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS nodes (
		label VARCHAR(80) PRIMARY KEY,
		ipmi_user VARCHAR(80) NOT NULL,
		ipmi_pass VARCHAR(80) NOT NULL,
		ipmi_addr VARCHAR(80) NOT NULL,
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
		nodes: make(map[string]*Node),
		db:    db,
	}, nil
}

// Set the node `nodeId`'s ipmi info to `info`. Create the node if it does not
// already exist.
func (s *State) SetNode(nodeId string, info IpmiInfo) error {
	node, err := s.GetNode(nodeId)
	if err != nil {
		// If the node doesn't exist, create it:
		node = NewNode(info)
		s.nodes[nodeId] = node
		_, err := s.db.Exec(
			`INSERT INTO nodes(label, ipmi_user, ipmi_pass, ipmi_addr, version)
		VALUES (?, ?, ?, ?, 0)`, nodeId, info.User, info.Pass, info.Addr,
		)
		if err != nil {
			return err
		}
	}
	return node.BumpVersion(s.db)
}

// Get the node `nodeId`. Returns nil and an error if the node is not found.
func (s *State) GetNode(nodeId string) (*Node, error) {
	node, ok := s.nodes[nodeId]
	if !ok {
		// If we don't have the node cached, try to fetch it from the
		// database:
		node = NewNode(IpmiInfo{})
		row := s.db.QueryRow(
			`SELECT version, ipmi_user, ipmi_pass, ipmi_addr
			FROM nodes
			WHERE label = ?`, nodeId)
		err := row.Scan(&node.Version,
			&node.Ipmi.User,
			&node.Ipmi.Pass,
			&node.Ipmi.Addr)
		if err != nil {
			return nil, err
		}

		// Cache it for future use:
		s.nodes[nodeId] = node
	}
	return node, nil
}

// Delete node `nodeId`.
func (s *State) DelNode(nodeId string) error {
	_, ok := s.nodes[nodeId]
	if ok {
		node := s.nodes[nodeId]
		delete(s.nodes, nodeId)
		node.Disconnect()
	}
	_, err := s.db.Exec(`DELETE FROM nodes WHERE label = ?`, nodeId)
	return err
}
