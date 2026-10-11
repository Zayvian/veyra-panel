package sub

import (
	"encoding/base64"
	"fmt"
	"github.com/Zayvian/veyra-panel/internal/store"
	"strings"
)

// GB follows the panel quota input convention (1024^3 bytes).
func GB(n int64) string { return fmt.Sprintf("%.2f GB", float64(n)/(1<<30)) }
func ShadowrocketBase64(entries []Entry, u *store.User) string {
	total := GB(u.TrafficLimit)
	if u.TrafficLimit == 0 {
		total = "不限"
	}
	status := fmt.Sprintf("STATUS=上传：%s | 下载：%s | 总量：%s", GB(u.TrafficUp), GB(u.TrafficDown), total)
	if u.ExpiresAt != nil {
		status += " | 到期：" + u.ExpiresAt.Local().Format("2006-01-02 15:04")
	}
	return base64.StdEncoding.EncodeToString([]byte(status + "\r\n" + strings.Join(ShareLinks(entries), "\r\n")))
}
