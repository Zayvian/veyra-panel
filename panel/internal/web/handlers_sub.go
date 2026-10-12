package web

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Zayvian/veyra-panel/internal/store"
	"github.com/Zayvian/veyra-panel/internal/sub"
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
	// The operator's announcement goes at the top of the list, where clients
	// show it first.
	lines, err := s.svc.Announcement()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	// An inactive account gets one entry naming the reason rather than an empty
	// list, which every client presents as a broken subscription. The reason
	// comes first; the announcement after it, since it is often where to get
	// in touch.
	if notice := sub.Notice(sb.User, time.Now()); notice != nil {
		entries = append(notice, sub.Announcement(lines, notice)...)
	} else {
		entries = append(sub.Announcement(lines, entries), entries...)
	}

	format := sub.Detect(r)

	// Clients read usage and expiry from this header, so it goes on every
	// subscription format.
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
	case sub.FormatSingBox:
		data, err := sub.SingBox(entries, s.subscriptionOrigin(r))
		if err != nil {
			s.fail(w, r, err)
			return
		}
		w.Write(data)

	case sub.FormatClash:
		data, err := sub.Clash(entries, s.subscriptionOrigin(r))
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

// getRuleFile serves one of the routing lists a Clash or sing-box subscription
// downloads. They are public data; only the names routing.go uses exist.
func (s *Server) getRuleFile(w http.ResponseWriter, r *http.Request) {
	data, err := s.rules.Get(r.Context(), r.PathValue("file"))
	if errors.Is(err, sub.ErrUnknownRuleFile) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.log.Warn("rule file unavailable", "file", r.PathValue("file"), "err", err)
		http.Error(w, "rule file temporarily unavailable", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Write(data)
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
