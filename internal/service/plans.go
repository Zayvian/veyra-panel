package service

import (
	"strings"
	"unicode"

	"github.com/Zayvian/veyra-panel/internal/store"
)

// Plans are presets for the user form. A user made from one copies its values
// and keeps no link to it, so changing a price list never touches an account
// that has already been sold.

func (s *Service) Plans() ([]*store.Plan, error) { return s.st.Plans() }

func (s *Service) Plan(id int64) (*store.Plan, error) { return s.st.Plan(id) }

func (s *Service) CreatePlan(p *store.Plan) error {
	if err := checkPlan(p); err != nil {
		return err
	}
	return s.st.CreatePlan(p)
}

func (s *Service) UpdatePlan(p *store.Plan) error {
	if err := checkPlan(p); err != nil {
		return err
	}
	return s.st.UpdatePlan(p)
}

func (s *Service) DeletePlan(id int64) error { return s.st.DeletePlan(id) }

// checkPlan is looser about names than users are: a plan name is only ever
// shown in the panel — never sent to a node — so 「月付 100G」 is fine.
func checkPlan(p *store.Plan) error {
	p.Name = strings.TrimSpace(p.Name)
	p.Note = strings.TrimSpace(p.Note)
	if n := len([]rune(p.Name)); n == 0 || n > 32 {
		return invalid("plan name must be 1-32 characters")
	}
	for _, r := range p.Name {
		if unicode.IsControl(r) {
			return invalid("plan name cannot contain control characters")
		}
	}
	if p.Days < 0 || p.Days > MaxRenewDays {
		return invalid("plan validity must be between 0 and %d days", MaxRenewDays)
	}
	if p.TrafficLimit < 0 {
		return invalid("traffic limit cannot be negative")
	}
	if p.IPLimit < 0 {
		return invalid("address limit cannot be negative")
	}
	if p.ResetDay != store.PlanResetOnCreation {
		p.ResetDay = ClampResetDay(p.ResetDay)
	}
	return nil
}
