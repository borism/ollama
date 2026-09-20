// Package cluster implements LAN autodiscovery and workload sharing between
// ollama-cluster instances: when a model doesn't fit local VRAM (or the user
// explicitly asks), it spills onto another instance's spare GPU via
// llama.cpp RPC (api.Runner.RPCServers, see llm/llama_server.go's
// appendRPCArgs). Hub-shaped per request, like the rest of llama.cpp RPC
// (no worker-to-worker traffic, the head is the hub) -- any
// instance can be the transient head for one request while discoverable as
// a worker for someone else's, at the same time.
package cluster

import (
	"time"

	"github.com/borism/ollama-cluster/ml"
)

// Peer is one other ollama-cluster instance seen on the LAN.
type Peer struct {
	// ID is a random per-process identifier so a node can ignore its own
	// broadcasts (it will see them via the same UDP broadcast it sent).
	ID string

	// Addr is the source IP the beacon was seen from.
	Addr string

	// RPCPort is the ggml-rpc-server port on Addr, or 0 if this peer is
	// not currently willing to share (OLLAMA_CLUSTER_SHARE=0).
	RPCPort int

	// Devices is this peer's self-reported local devices (same shape
	// discover.GPUDevices returns locally).
	Devices []ml.DeviceInfo

	// ProtoMajor/ProtoMinor is the RPC_PROTO_MAJOR_VERSION/minor this
	// peer's ggml-rpc-server speaks (ggml/include/ggml-rpc.h). A head must
	// only use peers whose major matches its own and whose minor is <=
	// its own (enforced at handshake).
	ProtoMajor int
	ProtoMinor int

	// Load is a coarse 0..1 self-reported utilization, a secondary signal
	// alongside Devices[].FreeMemory to avoid preferring a peer that is
	// technically not full but is busy.
	Load float64

	// LastSeen is when we last heard this peer's beacon.
	LastSeen time.Time
}

// Stale reports whether this peer hasn't been heard from recently enough to
// still be trusted as available.
func (p Peer) Stale(now time.Time, ttl time.Duration) bool {
	return now.Sub(p.LastSeen) > ttl
}

// Config configures the discovery beacon (see discovery.go).
type Config struct {
	// Port is the UDP port used for both sending and receiving beacons.
	Port int

	// Interval is how often we announce our own presence.
	Interval time.Duration

	// TTL is how long since LastSeen before a peer is dropped from the table.
	TTL time.Duration

	// SelfID is this process's own random ID (see Peer.ID).
	SelfID string

	// SelfDevices is called fresh on every announce to get current local
	// device state (wire it to discover.GPUDevices).
	SelfDevices func() []ml.DeviceInfo

	// SelfRPCPort returns the current ggml-rpc-server port, or 0 if not
	// sharing right now (wire it to a *llm.RPCWorker's Addr()).
	SelfRPCPort func() int

	// SelfLoad returns this instance's own current 0..1 utilization.
	SelfLoad func() float64

	// Seeds is an optional list of "host:port" discovery-beacon addresses
	// to unicast our announcement to directly, in addition to the LAN
	// broadcast (see broadcastAddrs in discovery.go) -- the way to reach a
	// peer outside this host's broadcast domain (UDP broadcast doesn't
	// cross a subnet/VLAN boundary). Typically only a head needs seeds
	// configured, pointing at cross-subnet workers: a worker that learns
	// about the head via a seeded announcement never needs to reach other
	// workers back (see the package doc -- hub-shaped, no worker-to-worker
	// traffic). A seed that fails to resolve is logged and skipped, not
	// fatal to Start.
	Seeds []string
}
