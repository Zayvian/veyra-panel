package service

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/kosje/skysbx-panel/internal/store"
)

const (
	settingSubscriptionRefreshAt    = "subscription.force_refresh_at"
	settingSubscriptionRefreshUsers = "subscription.force_refresh_users"
	settingSubscriptionRefreshActor = "subscription.force_refresh_actor"
)

// SubscriptionRefresh records the most recent global credential rotation.
// The timestamp is an audit record for the operator, not an expiry or quota
// boundary for users.
type SubscriptionRefresh struct {
	At        *time.Time
	UserCount int
	Actor     string
	NodeCount int
}

// ForceSubscriptionRefresh rotates every protocol credential while preserving
// every user's subscription URL. Nodes receive a full config rebuild (rather
// than only a hot user-list update) so existing tunnels are dropped as well as
// future logins being rejected. A client that refreshes its saved subscription
// URL gets the new credentials immediately.
func (s *Service) ForceSubscriptionRefresh(actor string) (SubscriptionRefresh, error) {
	users, err := s.st.Users()
	if err != nil {
		return SubscriptionRefresh{}, err
	}
	if len(users) == 0 {
		return SubscriptionRefresh{}, invalid("there are no users to refresh")
	}
	nodes, err := s.st.Nodes()
	if err != nil {
		return SubscriptionRefresh{}, err
	}

	rotated := make([]store.UserCredentials, 0, len(users))
	for _, u := range users {
		rotated = append(rotated, store.UserCredentials{
			ID:         u.ID,
			VlessUUID:  NewUUID(),
			Password:   NewPassword(),
			SSPassword: NewSSPassword(),
		})
	}
	at := time.Now().UTC().Truncate(time.Second)
	actor = strings.TrimSpace(actor)
	if err := s.st.RotateAllUserCredentials(rotated, map[string]string{
		settingSubscriptionRefreshAt:    strconv.FormatInt(at.Unix(), 10),
		settingSubscriptionRefreshUsers: strconv.Itoa(len(rotated)),
		settingSubscriptionRefreshActor: actor,
	}); err != nil {
		return SubscriptionRefresh{}, err
	}

	// ConfigChanged sends config and users together. Rebuilding listeners is
	// intentional here: it closes existing authenticated streams, so holding an
	// old config cannot keep a tunnel alive until the user happens to reconnect.
	for _, node := range nodes {
		s.notify.ConfigChanged(node.ID)
	}
	return SubscriptionRefresh{At: &at, UserCount: len(rotated), Actor: actor, NodeCount: len(nodes)}, nil
}

func (s *Service) SubscriptionRefreshStatus() (SubscriptionRefresh, error) {
	rawAt, err := s.st.Setting(settingSubscriptionRefreshAt)
	if err != nil {
		return SubscriptionRefresh{}, err
	}
	rawCount, err := s.st.Setting(settingSubscriptionRefreshUsers)
	if err != nil {
		return SubscriptionRefresh{}, err
	}
	actor, err := s.st.Setting(settingSubscriptionRefreshActor)
	if err != nil {
		return SubscriptionRefresh{}, err
	}
	if rawAt == "" {
		return SubscriptionRefresh{Actor: actor}, nil
	}
	unix, err := strconv.ParseInt(rawAt, 10, 64)
	if err != nil {
		return SubscriptionRefresh{}, fmt.Errorf("stored subscription refresh time is corrupt: %w", err)
	}
	count, err := strconv.Atoi(rawCount)
	if err != nil || count < 0 {
		return SubscriptionRefresh{}, fmt.Errorf("stored subscription refresh user count is corrupt")
	}
	at := time.Unix(unix, 0).UTC()
	return SubscriptionRefresh{At: &at, UserCount: count, Actor: actor}, nil
}
