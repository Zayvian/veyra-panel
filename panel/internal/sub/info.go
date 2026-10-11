package sub

import (
	"encoding/base64"
	"fmt"
)

// humanBytes formats to two significant places, the way a client shows a quota.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit && exp < 4; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %ciB", float64(n)/float64(div), "KMGTP"[exp])
}

// ProfileTitle is the Profile-Title header. Clients that honour it show this
// instead of the raw subscription URL, which is otherwise what the user sees in
// their list of profiles.
func ProfileTitle(name string) string {
	return "base64:" + base64.StdEncoding.EncodeToString([]byte(name))
}
