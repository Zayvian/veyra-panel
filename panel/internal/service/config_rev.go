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
func ConfigRev(cfg *singbox.Config) (string, error) {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// NodeConfigRev is the rev of the configuration this node should be running.
func (s *Service) NodeConfigRev(nodeID int64) (string, error) {
	cfg, err := s.NodeConfig(nodeID)
	if err != nil {
		return "", err
	}
	return ConfigRev(cfg)
}
