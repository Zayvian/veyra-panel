package service

import (
	"strconv"
	"strings"
	"time"

	"github.com/Zayvian/veyra-panel/internal/store"
)

// ValidateHopping normalizes the UI representation used by URI and Mihomo.
func ValidateHopping(protocol, ports, interval, address string, relayID int64) (string, string, error) {
	ports = strings.TrimSpace(ports)
	if ports == "" {
		return "", "", nil
	}
	if protocol != store.ProtoHysteria2 {
		return "", "", invalid("port hopping is only supported for hysteria2")
	}
	if address != "" || relayID != 0 {
		return "", "", invalid("hysteria2 port hopping currently requires a direct node connection")
	}
	ranges, err := parseHopPorts(ports)
	if err != nil {
		return "", "", err
	}
	var normalized []string
	for _, r := range ranges {
		text := strconv.Itoa(r[0])
		if r[1] != r[0] {
			text += "-" + strconv.Itoa(r[1])
		}
		normalized = append(normalized, text)
	}
	if interval == "" {
		interval = "30s"
	}
	d, err := time.ParseDuration(interval)
	if err != nil || d < 5*time.Second || d > 24*time.Hour || d%time.Second != 0 {
		return "", "", invalid("hop interval must be whole seconds between 5s and 24h")
	}
	return strings.Join(normalized, ","), d.String(), nil
}

func parseHopPorts(value string) ([][2]int, error) {
	if len(value) > 512 {
		return nil, invalid("port range is too long")
	}
	var ranges [][2]int
	for _, part := range strings.Split(strings.ReplaceAll(value, ":", "-"), ",") {
		bounds := strings.Split(strings.TrimSpace(part), "-")
		if len(bounds) > 2 {
			return nil, invalid("invalid port range")
		}
		low, err := strconv.Atoi(strings.TrimSpace(bounds[0]))
		if err != nil || low < 1 || low > 65535 {
			return nil, invalid("invalid port range")
		}
		high := low
		if len(bounds) == 2 {
			high, err = strconv.Atoi(strings.TrimSpace(bounds[1]))
		}
		if err != nil || high < low || high > 65535 {
			return nil, invalid("invalid port range")
		}
		for _, r := range ranges {
			if low <= r[1] && high >= r[0] {
				return nil, invalid("overlapping port ranges")
			}
		}
		ranges = append(ranges, [2]int{low, high})
	}
	return ranges, nil
}

func (s *Service) checkHopConflicts(candidate *store.Inbound) error {
	all, err := s.st.Inbounds()
	if err != nil {
		return err
	}
	c, err := ParseClient(candidate)
	if err != nil {
		return err
	}
	var ownRanges [][2]int
	if c.HopPorts != "" {
		ownRanges, err = parseHopPorts(c.HopPorts)
		if err != nil {
			return err
		}
	}
	for _, other := range all {
		if other.ID == candidate.ID {
			continue
		}
		if other.NodeID == candidate.NodeID {
			oc, err := ParseClient(other)
			if err != nil {
				return err
			}
			if c.HopPorts != "" && overlapsPort(ownRanges, other.Port) {
				return invalid("hop ports overlap inbound %s", other.Tag)
			}
			if oc.HopPorts != "" {
				ranges, err := parseHopPorts(oc.HopPorts)
				if err != nil {
					return err
				}
				if overlapsPort(ranges, candidate.Port) {
					return invalid("port overlaps hop range of %s", other.Tag)
				}
				for _, r := range ownRanges {
					for _, o := range ranges {
						if r[0] <= o[1] && r[1] >= o[0] {
							return invalid("hop ranges overlap %s", other.Tag)
						}
					}
				}
			}
		}
		if other.RelayNodeID == candidate.NodeID && overlapsPort(ownRanges, other.RelayPort) {
			return invalid("hop ports overlap relay %s", other.Tag)
		}
		if candidate.RelayNodeID != 0 && other.NodeID == candidate.RelayNodeID {
			oc, err := ParseClient(other)
			if err != nil {
				return err
			}
			if oc.HopPorts != "" {
				ranges, err := parseHopPorts(oc.HopPorts)
				if err != nil {
					return err
				}
				if overlapsPort(ranges, candidate.RelayPort) {
					return invalid("relay port overlaps hop range of %s", other.Tag)
				}
			}
		}
	}
	return nil
}

func overlapsPort(ranges [][2]int, port int) bool {
	for _, r := range ranges {
		if port >= r[0] && port <= r[1] {
			return true
		}
	}
	return false
}
