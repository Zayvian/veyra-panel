package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/Zayvian/veyra-panel/internal/singbox"
)

// ConfigRev identifies a node configuration: the SHA-256 of its JSON.
//
// Sent with the configuration and echoed back by a node that supports
// config_rev once it is running it, so the panel can tell a configuration that
// took effect from one still on its way or refused. The node reports which
// inbound tags it serves either way; only the rev says whether those inbounds
// have the settings the panel last sent — a changed port or certificate keeps
// its tag.
//
// encoding/json writes struct fields in declaration order and map keys sorted,
// so the same configuration always has the same rev.
//
// xray is the Xray part, if the node is sent one. Without it the rev is the
// hash of the sing-box part alone, as it was before Xray, so no node sees its
// configuration change on upgrade.
func ConfigRev(cfg *singbox.Config, xray json.RawMessage) (string, error) {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	h.Write(raw)
	if len(xray) > 0 {
		h.Write([]byte("\nxray\n"))
		h.Write(xray)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// NodeConfigRev is the rev of the configuration this node should be running.
// xray says whether the node is sent its Xray part, which it is only when it
// offered xray.
func (s *Service) NodeConfigRev(nodeID int64, xray bool) (string, error) {
	cfg, err := s.NodeConfig(nodeID)
	if err != nil {
		return "", err
	}
	var x json.RawMessage
	if xray {
		if x, err = s.NodeXrayConfig(nodeID); err != nil {
			return "", err
		}
	}
	return ConfigRev(cfg, x)
}
