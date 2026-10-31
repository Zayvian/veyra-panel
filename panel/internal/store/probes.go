package store

// ProbeNodes is the set of nodes marked as domestic probes.
func (s *Store) ProbeNodes() (map[int64]bool, error) {
	rows, err := s.db.Query(`SELECT node_id FROM probe_nodes`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// SetProbeNode marks or unmarks a node as a domestic probe.
func (s *Store) SetProbeNode(nodeID int64, probe bool) error {
	if probe {
		_, err := s.db.Exec(`INSERT OR IGNORE INTO probe_nodes (node_id) VALUES (?)`, nodeID)
		return err
	}
	_, err := s.db.Exec(`DELETE FROM probe_nodes WHERE node_id = ?`, nodeID)
	return err
}
