package sub

import "strings"

// The routing both full-config formats share: which proxy groups a client sees
// and which traffic each one carries. Clash and sing-box spell it differently;
// this is the one place that says what it is.
//
// The rule data is MetaCubeX/meta-rules-dat, compiled from the v2fly domain
// list and the usual China IP list. The panel relays it (rulefiles.go) and
// clients refresh it daily; nobody maintains lists by hand.

const (
	groupNotice  = "公告"
	groupProxy   = "节点选择"
	groupAI      = "AI"
	groupYouTube = "YouTube"
	groupNetflix = "Netflix"
	groupGoogle  = "谷歌"
	groupApple   = "苹果"
	groupMS      = "微软"
	groupFinal   = "漏网之鱼"
)

// routeGroup is a selectable group for one kind of traffic. Direct-first groups
// default to going direct; the rest default to following the main group.
type routeGroup struct {
	name        string
	directFirst bool
}

var serviceGroups = []routeGroup{
	{groupAI, false},
	{groupYouTube, false},
	{groupNetflix, false},
	{groupGoogle, false},
	{groupApple, true},
	{groupMS, true},
}

// reservedNames are names a proxy may not take: they are groups (or, for
// DIRECT, built in) and a clash between the two fails the whole file.
var reservedNames = map[string]bool{
	groupNotice: true, groupProxy: true, groupAI: true, groupYouTube: true,
	groupNetflix: true, groupGoogle: true, groupApple: true, groupMS: true,
	groupFinal: true, "DIRECT": true, "REJECT": true,
}

// ruleSet is one list from meta-rules-dat. kind is "geosite" (domains) or
// "geoip" (addresses).
type ruleSet struct {
	kind, name string
}

// tag names the list in the generated file: "geosite-ai" rather than the
// upstream "category-ai-!cn", whose '!' is awkward in a file name.
func (r ruleSet) tag() string {
	return r.kind + "-" + strings.TrimSuffix(strings.TrimPrefix(r.name, "category-"), "-!cn")
}

// routeRule sends what a set of lists matches to a target: a group name, or
// targetDirect.
type routeRule struct {
	sets   []ruleSet
	target string
}

const targetDirect = "DIRECT"

// routeRules is the order traffic is matched in, first match wins; private
// addresses go direct before any of it, and anything unmatched goes to
// groupFinal after.
//
// Order matters where lists overlap: AI before Google (Gemini is in both),
// YouTube before Google, GitHub before Microsoft (Microsoft's list includes
// GitHub, which should not go direct by default). The China IP list comes
// last because checking it means resolving the name first.
var routeRules = []routeRule{
	{[]ruleSet{{"geosite", "category-ai-!cn"}}, groupAI},
	{[]ruleSet{{"geosite", "youtube"}}, groupYouTube},
	{[]ruleSet{{"geosite", "netflix"}, {"geoip", "netflix"}}, groupNetflix},
	{[]ruleSet{{"geosite", "google"}, {"geoip", "google"}}, groupGoogle},
	{[]ruleSet{{"geosite", "github"}}, groupProxy},
	{[]ruleSet{{"geosite", "apple"}}, groupApple},
	{[]ruleSet{{"geosite", "microsoft"}}, groupMS},
	{[]ruleSet{{"geosite", "cn"}}, targetDirect},
	{[]ruleSet{{"geoip", "cn"}}, targetDirect},
}

// ruleSetURL is where the panel fetches a list. Branch "meta" holds mihomo's
// .mrs files, "sing" sing-box's .srs. GitHub itself rather than a mirror:
// jsDelivr refuses the sing branch as too large.
func ruleSetURL(branch, ext string, r ruleSet) string {
	return "https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/" +
		branch + "/geo/" + r.kind + "/" + r.name + "." + ext
}

// groupMembers lists a service group's choices: the main group or direct
// first, depending on its default, then every node.
func groupMembers(g routeGroup, nodes []string) []string {
	first := []string{groupProxy, targetDirect}
	if g.directFirst {
		first = []string{targetDirect, groupProxy}
	}
	return append(first, nodes...)
}

// splitInfo separates real nodes from info entries (announcements, notices).
// With no real nodes — an inactive account — the info entries stand in, so
// the main group still has members to show the notice in.
func splitInfo(kept []Entry) (nodes, info []string) {
	for _, e := range kept {
		if e.Info {
			info = append(info, e.Name)
		} else {
			nodes = append(nodes, e.Name)
		}
	}
	if len(nodes) == 0 {
		return info, nil
	}
	return nodes, info
}
