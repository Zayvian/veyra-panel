package store

import (
	"database/sql"
	"fmt"
)

// PlanResetOnCreation is the reset day that means "whatever day of the month
// the user is created on", resolved when a user is made from the plan.
const PlanResetOnCreation = -1

// Plan is a preset for the user form. See migrations/0011_plans.sql.
type Plan struct {
	ID           int64
	Name         string
	Days         int   // 0 = no expiry
	TrafficLimit int64 // bytes, 0 = unlimited
	IPLimit      int
	ResetDay     int // 0 = never, PlanResetOnCreation, or 1-31
	Note         string
}

func (s *Store) CreatePlan(p *Plan) error {
	res, err := s.db.Exec(`INSERT INTO plans
		(name, days, traffic_limit, ip_limit, reset_day, note, created_at)
		VALUES (?, ?, ?, ?, ?, ?, unixepoch())`,
		p.Name, p.Days, p.TrafficLimit, p.IPLimit, p.ResetDay, p.Note)
	if err != nil {
		return asConflict(fmt.Errorf("create plan %q: %w", p.Name, err))
	}
	p.ID, _ = res.LastInsertId()
	return nil
}

func (s *Store) UpdatePlan(p *Plan) error {
	res, err := s.db.Exec(`UPDATE plans SET name = ?, days = ?, traffic_limit = ?,
		ip_limit = ?, reset_day = ?, note = ? WHERE id = ?`,
		p.Name, p.Days, p.TrafficLimit, p.IPLimit, p.ResetDay, p.Note, p.ID)
	if err != nil {
		return asConflict(fmt.Errorf("update plan %d: %w", p.ID, err))
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) Plan(id int64) (*Plan, error) {
	var p Plan
	err := s.db.QueryRow(`SELECT id, name, days, traffic_limit, ip_limit, reset_day, note
		FROM plans WHERE id = ?`, id).
		Scan(&p.ID, &p.Name, &p.Days, &p.TrafficLimit, &p.IPLimit, &p.ResetDay, &p.Note)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return &p, err
}

// Plans lists every plan, shortest first, which is the order a price list reads.
func (s *Store) Plans() ([]*Plan, error) {
	rows, err := s.db.Query(`SELECT id, name, days, traffic_limit, ip_limit, reset_day, note
		FROM plans ORDER BY days = 0, days, traffic_limit, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Plan
	for rows.Next() {
		var p Plan
		if err := rows.Scan(&p.ID, &p.Name, &p.Days, &p.TrafficLimit, &p.IPLimit, &p.ResetDay, &p.Note); err != nil {
			return nil, err
		}
		out = append(out, &p)
	}
	return out, rows.Err()
}

func (s *Store) DeletePlan(id int64) error {
	res, err := s.db.Exec(`DELETE FROM plans WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
