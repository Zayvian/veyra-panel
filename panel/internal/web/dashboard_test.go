package web

import (
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Zayvian/veyra-panel/internal/service"
	"github.com/Zayvian/veyra-panel/internal/store"
)

// A node with a stats reset day used to stop the dashboard template
// mid-table: every row after it, and the columns after 本周期, went missing.
func TestDashboardListsEveryNodeWithResetDay(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := service.New(st)
	a, _, err := svc.CreateNode("美国节点", "us.example.com", "US", 1000)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.CreateNode("日本节点", "jp.example.com", "JP", 1000); err != nil {
		t.Fatal(err)
	}
	a.StatsResetDay, a.StatsResetHour = 5, 8
	if err := svc.UpdateNode(a); err != nil {
		t.Fatal(err)
	}
	srv, err := New(svc, &fakeChannel{}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	srv.getDashboard(rec, httptest.NewRequest("GET", "/", nil))
	body := rec.Body.String()
	for _, want := range []string{"美国节点", "日本节点", "累计合计", "不重置"} {
		if !strings.Contains(body, want) {
			t.Fatalf("dashboard is missing %q", want)
		}
	}
}
