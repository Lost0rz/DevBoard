package hub

import (
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"strings"
)

const (
	minTokenLength      = 32
	maxTokenLength      = 128
	maxDisplayNameBytes = 64
)

// NodeConfig is the transient registration input used to build a Registry.
// The raw token is consumed at construction and is never retained afterwards.
type NodeConfig struct {
	NodeID      string
	DisplayName string
	Accent      string
	Enabled     bool
	Token       string
}

// Node is a registered hub-side node identity. It carries only the token
// digest, never the credential itself, and is immutable after construction.
type Node struct {
	ID          string
	DisplayName string
	Accent      string
	Enabled     bool
	tokenDigest [sha256.Size]byte
}

// Registry is the hub-side node identity authority. Node identity is the
// configured node_id plus per-node token; it never depends on an IP address.
type Registry struct {
	order []string
	nodes map[string]*Node
}

func NewRegistry(entries []NodeConfig) (*Registry, error) {
	r := &Registry{order: make([]string, 0, len(entries)), nodes: make(map[string]*Node, len(entries))}
	seenTokens := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if err := validateRegistryEntry(entry); err != nil {
			return nil, fmt.Errorf("node %q: %w", entry.NodeID, err)
		}
		if _, ok := r.nodes[entry.NodeID]; ok {
			return nil, fmt.Errorf("duplicate node id %q", entry.NodeID)
		}
		digest := sha256.Sum256([]byte(entry.Token))
		key := string(digest[:])
		if _, ok := seenTokens[key]; ok {
			return nil, fmt.Errorf("duplicate node token for %q", entry.NodeID)
		}
		seenTokens[key] = struct{}{}
		r.order = append(r.order, entry.NodeID)
		r.nodes[entry.NodeID] = &Node{ID: entry.NodeID, DisplayName: entry.DisplayName, Accent: entry.Accent, Enabled: entry.Enabled, tokenDigest: digest}
	}
	return r, nil
}

func (r *Registry) Lookup(nodeID string) (*Node, bool) {
	node, ok := r.nodes[nodeID]
	return node, ok
}

func (r *Registry) Order() []string {
	out := make([]string, len(r.order))
	copy(out, r.order)
	return out
}

// Authenticate resolves a presented bearer token to its registered node.
// Comparison is constant-time per node over token digests and scans the full
// registry without early exit. A disabled node still authenticates: disabling
// is reported to the receiver as a binding failure, not a missing credential.
func (r *Registry) Authenticate(token string) *Node {
	if len(token) == 0 || len(token) > maxTokenLength {
		return nil
	}
	digest := sha256.Sum256([]byte(token))
	var match *Node
	for _, node := range r.nodes {
		if subtle.ConstantTimeCompare(digest[:], node.tokenDigest[:]) == 1 {
			match = node
		}
	}
	return match
}

func validateRegistryEntry(entry NodeConfig) error {
	if !ValidNodeID(entry.NodeID) {
		return fmt.Errorf("node id is invalid")
	}
	if err := validateDisplayName(entry.DisplayName); err != nil {
		return err
	}
	if err := validateAccent(entry.Accent); err != nil {
		return err
	}
	if len(entry.Token) < minTokenLength || len(entry.Token) > maxTokenLength {
		return fmt.Errorf("token must be %d-%d characters", minTokenLength, maxTokenLength)
	}
	for i := 0; i < len(entry.Token); i++ {
		c := entry.Token[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '.' || c == '_' || c == '~' || c == '+' || c == '-':
		default:
			return fmt.Errorf("token contains unsupported characters")
		}
	}
	return nil
}

func validateAccent(accent string) error {
	accent = strings.ToLower(strings.TrimSpace(accent))
	if accent == "" {
		return nil
	}
	switch accent {
	case "blue", "cyan", "violet", "amber", "green":
		return nil
	default:
		return fmt.Errorf("accent is not allowed")
	}
}

func validateDisplayName(name string) error {
	if len(name) > maxDisplayNameBytes {
		return fmt.Errorf("display name exceeds %d bytes", maxDisplayNameBytes)
	}
	for i := 0; i < len(name); i++ {
		if name[i] < 0x20 || name[i] == 0x7f {
			return fmt.Errorf("display name contains control characters")
		}
		if name[i] == ',' || name[i] == '=' {
			return fmt.Errorf("display name contains reserved characters")
		}
	}
	return nil
}
