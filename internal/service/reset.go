package service

import (
	"time"

	"github.com/kosje/skysbx-panel/internal/store"
)

// A monthly traffic allowance.
//
// The alternative is the operator zeroing every account by hand every month,
// and an account they forget silently stops working the moment it crosses its
// limit — the same symptom as an expiry, from a different cause, with nothing
// on the page to tell the two apart.

// ClampResetDay bounds a day-of-month to something a schedule can use. Zero is
// "never", which is what an out-of-range value collapses to rather than being
// rejected: this is a number typed into a form, and silently meaning "no
// schedule" is the safe reading of nonsense.
func ClampResetDay(day int) int {
	if day < 1 || day > 31 {
		return 0
	}
	return day
}

// ClampResetTime bounds the clock portion of a local monthly schedule.
func ClampResetTime(hour, minute int) (int, int) {
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, 0
	}
	return hour, minute
}

// scheduledInMonth is the reset instant for one month, clamped to the last day
// when the month is shorter. February is not a month to skip.
func scheduledInMonth(year int, month time.Month, day int, loc *time.Location) time.Time {
	return scheduledInMonthAt(year, month, day, 0, 0, loc)
}

func scheduledInMonthAt(year int, month time.Month, day, hour, minute int, loc *time.Location) time.Time {
	// Day 0 of the next month is the last day of this one, which is how the
	// length of a month is found without a table.
	last := time.Date(year, month+1, 0, 0, 0, 0, 0, loc).Day()
	if day > last {
		day = last
	}
	hour, minute = ClampResetTime(hour, minute)
	return time.Date(year, month, day, hour, minute, 0, 0, loc)
}

// LastScheduledReset is the most recent reset instant at or before now.
//
// Local time, because the operator picked a day on a calendar and that is the
// calendar they are looking at — the same reason expiry dates are parsed in the
// local zone.
func LastScheduledReset(day int, now time.Time) time.Time {
	loc := now.Location()
	if c := scheduledInMonth(now.Year(), now.Month(), day, loc); !c.After(now) {
		return c
	}
	year, month := now.Year(), now.Month()-1
	if month < time.January {
		year, month = year-1, time.December
	}
	return scheduledInMonth(year, month, day, loc)
}

// NextScheduledReset is the next reset instant strictly after now, for display.
func NextScheduledReset(day int, now time.Time) time.Time {
	loc := now.Location()
	if c := scheduledInMonth(now.Year(), now.Month(), day, loc); c.After(now) {
		return c
	}
	year, month := now.Year(), now.Month()+1
	if month > time.December {
		year, month = year+1, time.January
	}
	return scheduledInMonth(year, month, day, loc)
}

// LastScheduledResetAt is LastScheduledReset with an operator-selected clock.
func LastScheduledResetAt(day, hour, minute int, now time.Time) time.Time {
	loc := now.Location()
	if c := scheduledInMonthAt(now.Year(), now.Month(), day, hour, minute, loc); !c.After(now) {
		return c
	}
	year, month := now.Year(), now.Month()-1
	if month < time.January {
		year, month = year-1, time.December
	}
	return scheduledInMonthAt(year, month, day, hour, minute, loc)
}

// NextScheduledResetAt is the next local calendar reset including its clock.
func NextScheduledResetAt(day, hour, minute int, now time.Time) time.Time {
	loc := now.Location()
	if c := scheduledInMonthAt(now.Year(), now.Month(), day, hour, minute, loc); c.After(now) {
		return c
	}
	year, month := now.Year(), now.Month()+1
	if month > time.December {
		year, month = year+1, time.January
	}
	return scheduledInMonthAt(year, month, day, hour, minute, loc)
}

// resetDue reports whether this user's counter should be zeroed now.
//
// A nil LastResetAt is *not* due. It means the cycle has no start point — a row
// that predates the column — and treating "never reset" as "overdue" would wipe
// the usage of every existing account the first time the sweep ran after an
// upgrade. RunDueResets gives it a start point instead, and the first real reset
// happens on the next scheduled day.
func resetDue(u *store.User, now time.Time) bool {
	if ClampResetDay(u.ResetDay) == 0 || u.LastResetAt == nil {
		return false
	}
	return u.LastResetAt.Before(LastScheduledResetAt(u.ResetDay, u.ResetHour, u.ResetMinute, now))
}

// RunDueResets zeroes the counter for every user whose reset day has passed
// since the last time theirs was zeroed, and reports how many.
//
// Comparing against the last reset rather than against a stored "next due" is
// what makes a missed month impossible: a panel switched off across someone's
// reset day performs it on the next start.
//
// An account already expired at the scheduled minute is left untouched: expiry
// has priority over a reset that shares its timestamp. The daily traffic table
// still keeps the immutable history used by the dashboard and node ledger.
func (s *Service) RunDueResets() (int, error) {
	users, err := s.st.Users()
	if err != nil {
		return 0, err
	}
	now := time.Now()
	expiredSinceLastCheck := s.expiriesPassed(users, now)

	n := 0
	for _, u := range users {
		// A schedule with no start point gets one, without losing anything.
		// This is the state every row is in immediately after the column was
		// added, so getting it wrong would wipe the whole panel once.
		if ClampResetDay(u.ResetDay) > 0 && u.LastResetAt == nil {
			if err := s.st.TouchUserReset(u.ID); err != nil {
				return n, err
			}
			continue
		}
		// An expiry wins over a reset at the same moment. This avoids clearing a
		// dead account just because a calendar boundary happened to coincide;
		// renewing it later starts from the ordinary schedule instead.
		if u.ExpiresAt != nil && !now.Before(*u.ExpiresAt) {
			continue
		}
		if !resetDue(u, now) {
			continue
		}
		if err := s.st.ResetUserTraffic(u.ID); err != nil {
			return n, err
		}
		n++
	}
	if n > 0 || expiredSinceLastCheck {
		// Crossing a limit revokes access by omission from the push, so coming
		// back under one has to be pushed as well or the account stays cut off
		// until some unrelated edit happens.
		s.notify.UsersChanged()
	}
	if _, err := s.RunDueNodeStats(now); err != nil {
		return n, err
	}
	return n, nil
}

// expiriesPassed reports whether a live node needs a new user list. On the
// first scheduler pass the nodes have not connected yet (or will fetch the
// list on connect), so there is nothing to push. Later passes cover each exact
// expiry instant crossed since the previous minute boundary.
func (s *Service) expiriesPassed(users []*store.User, now time.Time) bool {
	s.scheduleMu.Lock()
	previous := s.lastScheduleCheck
	s.lastScheduleCheck = now
	s.scheduleMu.Unlock()
	if previous.IsZero() || !now.After(previous) {
		return false
	}
	for _, u := range users {
		if u.ExpiresAt != nil && u.ExpiresAt.After(previous) && !u.ExpiresAt.After(now) {
			return true
		}
	}
	return false
}

// RunDueNodeStats resets each node's dashboard-period counter. It never
// deletes the traffic ledger or changes user quota accounting.
func (s *Service) RunDueNodeStats(now time.Time) (int, error) {
	nodes, err := s.st.Nodes()
	if err != nil {
		return 0, err
	}
	n := 0
	for _, node := range nodes {
		if ClampResetDay(node.StatsResetDay) == 0 {
			continue
		}
		if node.StatsLastResetAt == nil {
			if err := s.st.TouchNodeStats(node.ID); err != nil {
				return n, err
			}
			continue
		}
		last := LastScheduledResetAt(node.StatsResetDay, node.StatsResetHour, node.StatsResetMinute, now)
		if !node.StatsLastResetAt.Before(last) {
			continue
		}
		if err := s.st.ResetNodeStats(node.ID); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
