package web

import (
	"fmt"
	"time"
	_ "time/tzdata" // the panel is built without cgo and VPS images often lack zoneinfo

	"github.com/Zayvian/veyra-panel/internal/store"
)

const settingTimezone = "web.timezone"

// DefaultTimezone is what an unset panel uses. The panel is written for
// operators in China, and a fresh VPS almost always runs in UTC, which made
// every date on the panel eight hours off from what the operator expected.
const DefaultTimezone = "Asia/Shanghai"

// timezoneServer is the setting value for "use the server's own zone", the
// behaviour from before the panel had a zone of its own.
const timezoneServer = "server"

// timezoneChoices are the zones the settings page offers. Anything that
// time.LoadLocation accepts works if written to the database directly.
var timezoneChoices = []struct{ Value, Label string }{
	{"Asia/Shanghai", "北京时间（UTC+8）"},
	{"Asia/Hong_Kong", "香港（UTC+8）"},
	{"Asia/Taipei", "台北（UTC+8）"},
	{"Asia/Singapore", "新加坡（UTC+8）"},
	{"Asia/Tokyo", "东京（UTC+9）"},
	{"UTC", "UTC"},
	{"Europe/London", "伦敦"},
	{"America/Los_Angeles", "洛杉矶"},
	{"America/New_York", "纽约"},
	{timezoneServer, "跟随服务器系统时区"},
}

// serverLocal is time.Local as the process found it, before ApplyTimezone.
var serverLocal = time.Local

func loadTimezone(name string) (*time.Location, error) {
	switch name {
	case "":
		name = DefaultTimezone
	case timezoneServer:
		return serverLocal, nil
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("unknown time zone %q", name)
	}
	return loc, nil
}

// ApplyTimezone makes the panel's time zone the process's time.Local. Every
// date the panel shows or reads from a form, and every calendar-day sum (plan
// days, renewals, monthly resets), goes through time.Local, so this one
// switch moves all of them together.
//
// It must run before anything else starts: time.Local is a plain variable
// and is not safe to change while other goroutines read it. That is also
// why changing the zone from the settings page restarts the panel.
func ApplyTimezone(st *store.Store) (string, error) {
	name, err := st.Setting(settingTimezone)
	if err != nil {
		return "", err
	}
	loc, err := loadTimezone(name)
	if err != nil {
		return "", err
	}
	time.Local = loc
	if name == "" {
		name = DefaultTimezone
	}
	return name, nil
}
