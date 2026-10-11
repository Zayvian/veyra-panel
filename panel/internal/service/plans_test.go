package service

import (
	"errors"
	"strings"
	"testing"

	"github.com/Zayvian/veyra-panel/internal/store"
)

func TestPlanLifecycle(t *testing.T) {
	svc := newTestService(t)
	p := &store.Plan{Name: " 月付 100G ", Days: 30, TrafficLimit: 100 << 30, IPLimit: 3,
		ResetDay: store.PlanResetOnCreation, Note: "¥30"}
	if err := svc.CreatePlan(p); err != nil {
		t.Fatal(err)
	}
	if p.Name != "月付 100G" {
		t.Fatalf("name not trimmed: %q", p.Name)
	}
	got, err := svc.Plan(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if *got != *p {
		t.Fatalf("stored %+v, want %+v", got, p)
	}

	if err := svc.CreatePlan(&store.Plan{Name: "月付 100G"}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate name: %v, want conflict", err)
	}

	p.Days, p.ResetDay = 90, 15
	if err := svc.UpdatePlan(p); err != nil {
		t.Fatal(err)
	}
	if got, _ := svc.Plan(p.ID); got.Days != 90 || got.ResetDay != 15 {
		t.Fatalf("update: %+v", got)
	}

	long := &store.Plan{Name: "年付", Days: 365}
	open := &store.Plan{Name: "不限期"}
	for _, q := range []*store.Plan{long, open} {
		if err := svc.CreatePlan(q); err != nil {
			t.Fatal(err)
		}
	}
	plans, err := svc.Plans()
	if err != nil {
		t.Fatal(err)
	}
	// Shortest first, open-ended last.
	if len(plans) != 3 || plans[0].ID != p.ID || plans[1].ID != long.ID || plans[2].ID != open.ID {
		t.Fatalf("order: %+v", plans)
	}

	if err := svc.DeletePlan(p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Plan(p.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("deleted plan still there: %v", err)
	}
	if err := svc.DeletePlan(p.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
}

func TestPlanValidation(t *testing.T) {
	svc := newTestService(t)
	for _, p := range []*store.Plan{
		{Name: ""},
		{Name: "   "},
		{Name: strings.Repeat("长", 33)},
		{Name: "bad\nname"},
		{Name: "x", Days: -1},
		{Name: "x", Days: MaxRenewDays + 1},
		{Name: "x", TrafficLimit: -1},
		{Name: "x", IPLimit: -1},
	} {
		if err := svc.CreatePlan(p); !errors.Is(err, ErrInvalid) {
			t.Errorf("%+v: %v, want ErrInvalid", p, err)
		}
	}
}
