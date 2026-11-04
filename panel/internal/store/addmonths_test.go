package store

import (
	"testing"
	"time"
)

func TestAddMonths(t *testing.T) {
	at := func(y int, m time.Month, d int) time.Time {
		return time.Date(y, m, d, 23, 59, 0, 0, time.Local)
	}
	for _, tc := range []struct {
		from   time.Time
		months int
		want   time.Time
	}{
		{at(2026, 12, 4), 1, at(2027, 1, 4)},
		{at(2026, 11, 4), 1, at(2026, 12, 4)},
		{at(2027, 1, 31), 1, at(2027, 2, 28)},
		{at(2028, 1, 31), 1, at(2028, 2, 29)},
		{at(2026, 3, 31), 1, at(2026, 4, 30)},
		{at(2026, 8, 31), 6, at(2027, 2, 28)},
		{at(2026, 5, 15), 12, at(2027, 5, 15)},
		{at(2026, 5, 15), 0, at(2026, 5, 15)},
	} {
		if got := AddMonths(tc.from, tc.months); !got.Equal(tc.want) {
			t.Errorf("AddMonths(%s, %d) = %s, want %s", tc.from.Format(time.DateTime), tc.months,
				got.Format(time.DateTime), tc.want.Format(time.DateTime))
		}
	}
}
