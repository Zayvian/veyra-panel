package web

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Zayvian/veyra-panel/internal/service"
)

// An expiring account is listed with a renew button aimed at its own row, and
// renewing it — which re-renders the table — takes it off the list.
func TestAttentionListOnUsersPage(t *testing.T) {
	svc, c := newAdminClient(t)
	soon := time.Now().Add(48 * time.Hour)
	u, err := svc.CreateUser(service.NewUser{Name: "alice", ExpiresAt: &soon})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateUser(service.NewUser{Name: "bob"}); err != nil {
		t.Fatal(err)
	}

	body := c.do(http.MethodGet, "/users", nil).Body.String()
	if !strings.Contains(body, "需要处理（1）") || !strings.Contains(body, "后到期") {
		t.Fatalf("attention list missing: %s", body)
	}
	if !strings.Contains(body, fmt.Sprintf(`hx-target="#row-%d"`, u.ID)) ||
		!strings.Contains(body, fmt.Sprintf(`id="row-%d"`, u.ID)) {
		t.Fatal("renew button does not target the user's row")
	}

	rec := c.do(http.MethodPost, fmt.Sprintf("/users/%d/renew", u.ID), map[string][]string{"days": {"30"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("renew: %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "需要处理") {
		t.Fatal("renewed account still listed")
	}
}
