package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Zayvian/veyra-panel/internal/service"
	"github.com/Zayvian/veyra-panel/internal/store"
)

// The handshake and masquerade fields come prefilled, and a <datalist> only
// offers options matching what is typed — so the suggestions were unreachable.
// Each field now has a dropdown listing every suggested site.
func TestSitePickersOfferEveryCandidate(t *testing.T) {
	svc, c := newAdminClient(t)
	n, _, err := svc.CreateNode("tw", "tw.example.com", "TW")
	if err != nil {
		t.Fatal(err)
	}
	vless, err := svc.CreateInbound(n.ID, service.InboundSpec{Protocol: store.ProtoVLESS, Port: 443, Tag: "台湾 01"})
	if err != nil {
		t.Fatal(err)
	}
	hy2, err := svc.CreateInbound(n.ID, service.InboundSpec{Protocol: store.ProtoHysteria2, Port: 8443,
		Tag: "台湾 02", ServerName: "tw.example.com"})
	if err != nil {
		t.Fatal(err)
	}

	check := func(what, body string) {
		t.Helper()
		if strings.Contains(body, "<datalist") || strings.Contains(body, `list="`) {
			t.Errorf("%s still uses a datalist", what)
		}
		for _, site := range append(append([]string{}, service.HandshakeCandidates...), service.MasqueradeCandidates...) {
			if !strings.Contains(body, fmt.Sprintf(`<option value="%s">`, site)) {
				t.Errorf("%s does not offer %s", what, site)
			}
		}
	}

	// The whole page, as a browser navigating to it gets it.
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/nodes/%d/inbounds", n.ID), nil)
	req.AddCookie(c.session)
	rec := httptest.NewRecorder()
	c.h.ServeHTTP(rec, req)
	page := rec.Body.String()
	check("the create form", page)
	if got := strings.Count(page, `class="site-pick"`); got != 2 {
		t.Errorf("create form has %d site pickers, want 2 (handshake, masquerade)", got)
	}

	// The edit row arrives as an htmx fragment: the table, with one row as a
	// form carrying the picker its protocol uses.
	vlessEdit := c.do(http.MethodGet, fmt.Sprintf("/inbounds/%d/edit", vless.ID), nil).Body.String()
	if got := strings.Count(vlessEdit, `class="site-pick"`); got != 1 ||
		!strings.Contains(vlessEdit, `<option value="www.apple.com:443">`) {
		t.Errorf("VLESS edit row: %d pickers, want the handshake one", got)
	}
	hy2Edit := c.do(http.MethodGet, fmt.Sprintf("/inbounds/%d/edit", hy2.ID), nil).Body.String()
	if got := strings.Count(hy2Edit, `class="site-pick"`); got != 1 ||
		!strings.Contains(hy2Edit, `<option value="https://www.dell.com">`) {
		t.Errorf("Hysteria2 edit row: %d pickers, want the masquerade one", got)
	}
}
