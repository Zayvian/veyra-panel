package service

import (
	"fmt"
	"sort"
	"time"

	"github.com/Zayvian/veyra-panel/internal/store"
)

const (
	// AttentionAhead is how far ahead an upcoming expiry is flagged: a week is
	// long enough to remind a customer and hear back before it lapses.
	AttentionAhead = 7 * 24 * time.Hour
	// AttentionLapsed is how long a lapsed account stays on the list. After a
	// month a customer who has not renewed has left, and keeping them there
	// buries the ones who are about to.
	AttentionLapsed = 30 * 24 * time.Hour
	// AttentionUsage is the share of a traffic allowance that gets flagged.
	AttentionUsage = 0.9
)

// Attention is one account the operator should look at soon, and why.
type Attention struct {
	User   *store.User
	Reason string
	// Lapsed means the account has already stopped working, as opposed to
	// being about to.
	Lapsed bool
	// due orders the list: the soonest problem first.
	due time.Time
}

// NeedsAttention picks the accounts that are about to stop working, or did
// recently: expiring within AttentionAhead, expired within AttentionLapsed, or
// at AttentionUsage of their traffic. Disabled accounts are left out — someone
// switched those off on purpose. Ordered soonest first.
func NeedsAttention(users []*store.User, now time.Time) []Attention {
	var out []Attention
	for _, u := range users {
		if !u.Enabled {
			continue
		}
		if a, ok := attention(u, now); ok {
			out = append(out, a)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].due.Before(out[j].due) })
	return out
}

func attention(u *store.User, now time.Time) (Attention, bool) {
	if u.ExpiresAt != nil {
		left := u.ExpiresAt.Sub(now)
		switch {
		case left <= 0 && -left <= AttentionLapsed:
			reason := "刚刚到期"
			if -left >= time.Hour {
				reason = "已到期 " + span(-left)
			}
			return Attention{User: u, Reason: reason, Lapsed: true, due: *u.ExpiresAt}, true
		case left > 0 && left <= AttentionAhead:
			return Attention{User: u, Reason: span(left) + "后到期", due: *u.ExpiresAt}, true
		}
	}
	if u.TrafficLimit > 0 && float64(u.TrafficUsed) >= AttentionUsage*float64(u.TrafficLimit) {
		a := Attention{User: u, due: now.Add(AttentionAhead)}
		if u.TrafficUsed >= u.TrafficLimit {
			a.Reason, a.Lapsed, a.due = "流量已用完", true, now
		} else {
			a.Reason = fmt.Sprintf("流量已用 %d%%", u.TrafficUsed*100/u.TrafficLimit)
		}
		// A monthly allowance comes back by itself; say when, so it is not
		// mistaken for one that needs selling.
		if day, hour, minute := u.ResetSchedule(); day > 0 {
			a.Reason += "，" + NextScheduledResetAt(day, hour, minute, now).Format("01-02") + " 重置"
		}
		return a, true
	}
	return Attention{}, false
}

// span is a duration in the unit that reads naturally, to the nearest one:
// days, else hours, else "不到 1 小时". Nearest rather than truncated, so an
// account sold for three days a minute ago reads 3 天, not 2.
func span(d time.Duration) string {
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%d 天", int(d.Round(24*time.Hour)/(24*time.Hour)))
	case d >= time.Hour:
		return fmt.Sprintf("%d 小时", int(d.Round(time.Hour)/time.Hour))
	}
	return "不到 1 小时"
}
