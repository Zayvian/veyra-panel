package web

import (
	"time"

	"github.com/Zayvian/veyra-panel/internal/probe"
	"github.com/Zayvian/veyra-panel/internal/store"
)

// SetProbes attaches the reachability monitor whose verdicts the node and
// inbound pages show. Without one they show none.
func (s *Server) SetProbes(m *probe.Monitor) { s.probes = m }

// probeBadge is one verdict as a template shows it.
type probeBadge struct {
	Label, Class, Advice string
	Since                time.Time
	Probes               int
}

func badge(v probe.Verdict) probeBadge {
	return probeBadge{Label: v.Label(), Class: v.Class(), Advice: v.Advice()}
}

// inboundProbes is each inbound's verdict, for the ones a round has judged.
func (s *Server) inboundProbes(inbounds []*store.Inbound) map[int64]probeBadge {
	out := map[int64]probeBadge{}
	if s.probes == nil {
		return out
	}
	for _, in := range inbounds {
		if st, ok := s.probes.Inbound(in.ID); ok {
			b := badge(st.Verdict)
			b.Since, b.Probes = st.Since, st.Probes
			out[in.ID] = b
		}
	}
	return out
}

// nodeProbes is each node's worst inbound verdict.
func (s *Server) nodeProbes(nodes []*store.Node, inbounds []*store.Inbound) map[int64]probeBadge {
	out := map[int64]probeBadge{}
	if s.probes == nil {
		return out
	}
	for _, n := range nodes {
		if v, ok := s.probes.Node(n.ID, inbounds); ok {
			out[n.ID] = badge(v)
		}
	}
	return out
}
