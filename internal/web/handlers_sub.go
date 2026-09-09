package web

import (
	"errors"
	"net/http"
	"strings"

	"github.com/kosje/skysbx-panel/internal/service"
	"github.com/kosje/skysbx-panel/internal/store"
	"github.com/kosje/skysbx-panel/internal/sub"
)

// getSubscription serves a user's configuration in whatever format their client
// asked for. It is the only unauthenticated route that returns anything: the
// token in the path is the credential.
func (s *Server) getSubscription(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	sb, err := s.svc.Subscription(token)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		s.fail(w, r, err)
		return
	}

	entries, err := sub.Build(sb.User, sb.Nodes, sb.Inbounds, sb.Allowed)
	if err != nil {
		s.fail(w, r, err)
		return
	}

	format := sub.Detect(r)

	// Clients read usage and expiry from this header rather than opening the
	// page, so it goes on every format including the HTML one.
	var expires int64
	if sb.User.ExpiresAt != nil {
		expires = sb.User.ExpiresAt.Unix()
	}
	w.Header().Set("Subscription-Userinfo",
		sub.UserInfoHeader(sb.User.TrafficUp, sb.User.TrafficDown, sb.User.TrafficLimit, expires))
	// Announce where to refresh from, and how often, in the two headers the
	// major clients honour.
	w.Header().Set("Profile-Update-Interval", "12")
	w.Header().Set("Profile-Title", sub.ProfileTitle(sb.User.Name))
	w.Header().Set("Content-Type", sub.ContentType(format))

	switch format {
	case sub.FormatHTML:
		s.subscriptionPage(w, r, sb, entries)

	case sub.FormatSingBox:
		data, err := sub.SingBox(entries)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		w.Write(data)

	case sub.FormatClash:
		data, err := sub.Clash(entries)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		w.Write(data)

	default:
		if strings.Contains(strings.ToLower(r.UserAgent()), "shadowrocket") || strings.EqualFold(r.URL.Query().Get("format"), "shadowrocket") {
			// Use a single display source for Shadowrocket: STATUS supplies GB
			// text, while a competing raw-byte header can override that text.
			w.Header().Del("Subscription-Userinfo")
			w.Write([]byte(sub.ShadowrocketBase64(entries, sb.User)))
		} else {
			w.Write([]byte(sub.Base64(entries)))
		}
	}
}

func (s *Server) subscriptionPage(w http.ResponseWriter, r *http.Request,
	sb *service.Subscription, entries []sub.Entry,
) {
	links := sub.ShareLinks(entries)
	rows := make([]map[string]any, 0, len(entries))
	for i, e := range entries {
		rows = append(rows, map[string]any{
			"Name": e.Name, "Protocol": e.Protocol,
			"Address": e.Address, "Port": e.Port,
			"Link": links[i],
		})
	}

	var expires string
	if sb.User.ExpiresAt != nil {
		expires = sb.User.ExpiresAt.Format("2006-01-02")
	}

	s.render(w, "subscription", map[string]any{
		"User":       sb.User,
		"Entries":    rows,
		"Expires":    expires,
		"Used":       sb.User.TrafficUsed,
		"UploadGB":   sub.GB(sb.User.TrafficUp),
		"DownloadGB": sub.GB(sb.User.TrafficDown),
		"UsedGB":     sub.GB(sb.User.TrafficUsed),
		"LimitGB":    sub.GB(sb.User.TrafficLimit),
		"Limit":      sb.User.TrafficLimit,
		"SubURL":     s.subscriptionOrigin(r) + r.URL.Path,
		"Base64":     sub.Base64(entries),
		"Inactive":   len(entries) == 0,
	})
}

// subURL reconstructs the absolute URL this subscription was fetched from, so
// the page can offer it for copying.
//
// X-Forwarded-Proto is honoured because the panel may sit behind a proxy that
// terminated TLS — but only when it says one of the two things it is allowed to
// say. It is a request header, so the client picks its value, and putting an
// arbitrary one in front of "://" builds whatever scheme the client asked for.
func subURL(r *http.Request) string {
	scheme := "https"
	switch p := r.Header.Get("X-Forwarded-Proto"); p {
	case "http", "https":
		scheme = p
	default:
		if r.TLS == nil {
			scheme = "http"
		}
	}
	return scheme + "://" + r.Host + r.URL.Path
}
