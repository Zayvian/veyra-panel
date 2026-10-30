package sub

import (
	"time"

	"github.com/Zayvian/veyra-panel/internal/service"
	"github.com/Zayvian/veyra-panel/internal/store"
)

// noticeKey is a well-formed 2022-blake3-aes-256-gcm key that authenticates
// nobody. Clients validate the key length before they will import an entry, so
// it has to be the right shape even though it is never used.
const noticeKey = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="

// Notice is what an inactive user's subscription holds instead of nothing: one
// entry whose name says why the account stopped working.
//
// An empty subscription reads, in every client, as a broken subscription or a
// dead server, and the person on the other end tries reinstalling things before
// it occurs to them to ask. A named entry tells them in the one place they are
// already looking. It points at a closed loopback port, so selecting it fails
// at once and carries no traffic anywhere.
//
// Returns nil for an active user. The checks are in User.Active's order, so the
// reason given is the one that actually applies.
func Notice(u *store.User, now time.Time) []Entry {
	msg := noticeText(u, now)
	if msg == "" {
		return nil
	}
	return []Entry{infoEntry(msg)}
}

// Announcement turns the operator's announcement lines into entries for the
// top of the node list, skipping any line that would share its name with a
// real entry or a proxy group: clients refuse a whole configuration over a
// duplicate name.
func Announcement(lines []string, entries []Entry) []Entry {
	taken := make(map[string]bool, len(entries))
	for _, e := range entries {
		taken[e.Name] = true
	}
	out := make([]Entry, 0, len(lines))
	for _, line := range lines {
		if line == "" || taken[line] || reservedNames[line] {
			continue
		}
		taken[line] = true
		out = append(out, infoEntry(line))
	}
	return out
}

// infoEntry is an entry whose name is a message. Like the notice, it points at
// a closed loopback port, so selecting it fails at once and carries nothing.
func infoEntry(msg string) Entry {
	return Entry{
		Name:     msg,
		Label:    msg,
		Protocol: store.ProtoShadowsocks,
		Address:  "127.0.0.1",
		Port:     1,
		Method:   service.SSMethod,
		Password: noticeKey,
		Info:     true,
	}
}

func noticeText(u *store.User, now time.Time) string {
	switch {
	case !u.Enabled:
		return "账号已停用，请联系管理员"
	case u.ExpiresAt != nil && !now.Before(*u.ExpiresAt):
		return "已于 " + u.ExpiresAt.In(now.Location()).Format("2006-01-02 15:04") + " 到期，请联系续费"
	case u.TrafficLimit > 0 && u.TrafficUsed >= u.TrafficLimit:
		// A monthly allowance comes back by itself; saying when saves a message
		// asking whether it will.
		if day, hour, minute := u.ResetSchedule(); day > 0 {
			next := service.NextScheduledResetAt(day, hour, minute, now)
			return "本期流量已用完，" + next.Format("01-02 15:04") + " 自动恢复"
		}
		return "流量已用完，请联系续费"
	}
	return ""
}
