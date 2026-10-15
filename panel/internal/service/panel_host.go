package service

import (
	"encoding/json"
	"net"
	"strings"

	"github.com/Zayvian/veyra-panel/internal/store"
)

// HostMove counts what MovePanelHost rewrote.
type HostMove struct {
	Nodes    int
	Inbounds int
}

// MovePanelHost rewrites what pointed at the panel's own domain after the
// administrator changed it: a node whose address is that domain (the node
// installed on the panel's own server), a relay address naming it, and the
// server name of AnyTLS, Hysteria2 and TUIC inbounds that present it.
//
// Reality inbounds are left alone: their server name is the borrowed site's,
// never ours. Anything that names some other host is not the panel's to move.
func (s *Service) MovePanelHost(from, to string) (HostMove, error) {
	var moved HostMove
	from, to = strings.ToLower(from), strings.ToLower(to)
	if from == "" || to == "" || from == to {
		return moved, nil
	}

	nodes, err := s.st.Nodes()
	if err != nil {
		return moved, err
	}
	for _, n := range nodes {
		if !strings.EqualFold(n.Address, from) {
			continue
		}
		n.Address = to
		if err := s.UpdateNode(n); err != nil {
			return moved, err
		}
		moved.Nodes++
	}

	inbounds, err := s.st.Inbounds()
	if err != nil {
		return moved, err
	}
	for _, in := range inbounds {
		changed, err := moveInboundHost(in, from, to)
		if err != nil {
			return moved, err
		}
		if !changed {
			continue
		}
		if err := s.st.UpdateInbound(in); err != nil {
			return moved, err
		}
		s.notify.ConfigChanged(in.NodeID)
		s.notifyRelayHost(in.RelayNodeID)
		moved.Inbounds++
	}
	return moved, nil
}

func moveInboundHost(in *store.Inbound, from, to string) (bool, error) {
	changed := false
	if in.Address != "" {
		host, port, err := net.SplitHostPort(in.Address)
		if err != nil {
			host, port = in.Address, ""
		}
		if strings.EqualFold(host, from) {
			in.Address = to
			if port != "" {
				in.Address = net.JoinHostPort(to, port)
			}
			changed = true
		}
	}
	switch in.Protocol {
	case store.ProtoAnyTLS, store.ProtoHysteria2, store.ProtoTUIC:
	default:
		return changed, nil
	}
	sb, err := ParseConfig(in)
	if err != nil {
		return false, err
	}
	if sb.TLS == nil || sb.TLS.Reality != nil || !strings.EqualFold(sb.TLS.ServerName, from) {
		return changed, nil
	}
	client, err := ParseClient(in)
	if err != nil {
		return false, err
	}
	sb.TLS.ServerName = to
	if strings.EqualFold(client.SNI, from) {
		client.SNI = to
	}
	cfg, err := json.Marshal(sb)
	if err != nil {
		return false, err
	}
	cl, err := json.Marshal(client)
	if err != nil {
		return false, err
	}
	in.Config, in.Client = string(cfg), string(cl)
	return true, nil
}
