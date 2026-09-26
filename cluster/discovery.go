package cluster

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/borism/ollama-cluster/ml"
)

// RPCProtoMajor/RPCProtoMinor are the RPC_PROTO_MAJOR_VERSION/
// RPC_PROTO_MINOR_VERSION this build's vendored llama.cpp speaks
// (ggml/include/ggml-rpc.h at the LLAMA_CPP_VERSION tag). Bump these
// whenever LLAMA_CPP_VERSION changes the RPC protocol: peers advertise
// them, and a stale value pairs incompatible builds that then fail the
// RPC handshake at model load. 7.0.0 since b11081 ("rpc : hash-cache only
// weights").
const (
	RPCProtoMajor = 7
	RPCProtoMinor = 0
)

// announcement is the JSON payload broadcast on the wire. It's the subset
// of Peer this node can self-report; Addr and LastSeen are filled in by the
// receiver, not the sender.
type announcement struct {
	ID         string          `json:"id"`
	RPCPort    int             `json:"rpc_port"`
	Devices    []ml.DeviceInfo `json:"devices"`
	ProtoMajor int             `json:"proto_major"`
	ProtoMinor int             `json:"proto_minor"`
	Load       float64         `json:"load"`
}

// Table is a thread-safe, live set of peers seen via discovery beacons.
type Table struct {
	mu      sync.Mutex
	self    Peer
	peers   map[string]Peer
	ttl     time.Duration
	stopped chan struct{}
}

// Stopped is closed once discovery has shut down after its context was
// canceled and released its UDP port, so a restart can bind it again.
func (t *Table) Stopped() <-chan struct{} {
	return t.stopped
}

// Self returns our own current advertised state, for debugging/logging.
func (t *Table) Self() Peer {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.self
}

// Peers returns live peers, excluding stale ones per Config.TTL and
// excluding our own ID.
func (t *Table) Peers() []Peer {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	peers := make([]Peer, 0, len(t.peers))
	for _, p := range t.peers {
		if !p.Stale(now, t.ttl) {
			peers = append(peers, p)
		}
	}
	return peers
}

func (t *Table) updateSelf(p Peer) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.self = p
}

func (t *Table) observe(p Peer) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.peers[p.ID] = p
}

// setLatency records a freshly-probed round trip for an already-known
// peer (see probeLoop). A no-op if the peer expired between listing and
// probing -- nothing to update.
func (t *Table) setLatency(id string, d time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if p, ok := t.peers[id]; ok {
		p.Latency = d
		t.peers[id] = p
	}
}

// randomID returns a random hex string for use as a self ID, for
// convenience when Config.SelfID is left empty.
func randomID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand.Read on any real platform doesn't fail; fall back
		// to something unique-ish rather than erroring Start out.
		binary.BigEndian.PutUint64(b[:], uint64(time.Now().UnixNano()))
	}
	return fmt.Sprintf("%x", b)
}

// Start launches the discovery beacon: one goroutine periodically
// broadcasts our own state over UDP, one listens for peers' broadcasts and
// updates the returned Table. Both stop when ctx is canceled.
func Start(ctx context.Context, cfg Config) (*Table, error) {
	if cfg.SelfID == "" {
		cfg.SelfID = randomID()
	}

	conn, err := net.ListenUDP("udp4", &net.UDPAddr{Port: cfg.Port})
	if err != nil {
		return nil, fmt.Errorf("cluster: listen udp :%d: %w", cfg.Port, err)
	}

	t := &Table{
		peers:   make(map[string]Peer),
		ttl:     cfg.TTL,
		stopped: make(chan struct{}),
	}

	go broadcastLoop(ctx, conn, cfg, t)
	go listenLoop(ctx, conn, cfg, t)
	go probeLoop(ctx, t)

	go func() {
		<-ctx.Done()
		conn.Close()
		close(t.stopped)
	}()

	return t, nil
}

func selfAnnouncement(cfg Config) announcement {
	return announcement{
		ID:         cfg.SelfID,
		RPCPort:    cfg.SelfRPCPort(),
		Devices:    cfg.SelfDevices(),
		ProtoMajor: RPCProtoMajor,
		ProtoMinor: RPCProtoMinor,
		Load:       cfg.SelfLoad(),
	}
}

// broadcastAddrs returns the subnet-directed broadcast address (e.g.
// 192.168.1.255) of every up, non-loopback IPv4 interface, plus the global
// limited-broadcast address 255.255.255.255 as a best-effort fallback.
// Confirmed by hand against a real network: some LANs/routers deliver
// subnet-directed broadcast but silently drop 255.255.255.255, so that alone isn't enough
// -- and a multi-homed host (more than one NIC on the LAN) needs its own
// address computed per interface, not just the default route's.
func broadcastAddrs(port int) []*net.UDPAddr {
	addrs := []*net.UDPAddr{{IP: net.IPv4bcast, Port: port}}

	ifaces, err := net.Interfaces()
	if err != nil {
		return addrs
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagBroadcast == 0 {
			continue
		}
		ifaceAddrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range ifaceAddrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipnet.IP.To4()
			if ip4 == nil {
				continue
			}
			bcast := make(net.IP, 4)
			for i := range ip4 {
				bcast[i] = ip4[i] | ^ipnet.Mask[i]
			}
			addrs = append(addrs, &net.UDPAddr{IP: bcast, Port: port})
		}
	}
	return addrs
}

// resolveSeeds resolves Config.Seeds ("host:port" strings) to UDP
// addresses, logging and skipping any that fail to resolve rather than
// failing Start outright -- a typo'd or momentarily-unreachable seed
// shouldn't take down LAN broadcast discovery too.
func resolveSeeds(seeds []string) []*net.UDPAddr {
	addrs := make([]*net.UDPAddr, 0, len(seeds))
	for _, s := range seeds {
		addr, err := net.ResolveUDPAddr("udp4", s)
		if err != nil {
			slog.Warn("cluster: could not resolve seed, skipping", "seed", s, "error", err)
			continue
		}
		addrs = append(addrs, addr)
	}
	return addrs
}

// broadcastLoop periodically encodes and sends our own announcement.
func broadcastLoop(ctx context.Context, conn *net.UDPConn, cfg Config, t *Table) {
	dsts := append(broadcastAddrs(cfg.Port), resolveSeeds(cfg.Seeds)...)

	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()

	send := func() {
		a := selfAnnouncement(cfg)
		t.updateSelf(Peer{
			ID:         a.ID,
			RPCPort:    a.RPCPort,
			Devices:    a.Devices,
			ProtoMajor: a.ProtoMajor,
			ProtoMinor: a.ProtoMinor,
			Load:       a.Load,
			LastSeen:   time.Now(),
		})
		buf, err := json.Marshal(a)
		if err != nil {
			slog.Warn("cluster: encode announcement", "error", err)
			return
		}
		for _, dst := range dsts {
			if _, err := conn.WriteToUDP(buf, dst); err != nil {
				slog.Debug("cluster: broadcast announcement", "dst", dst, "error", err)
			}
		}
	}

	send()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			send()
		}
	}
}

// probeInterval/probeTimeout govern probeLoop's link-latency measurement
// to known peers, consumed by placement.go's water-fill.
const (
	probeInterval = 10 * time.Second
	probeTimeout  = 500 * time.Millisecond
)

// probeLoop periodically times a TCP dial to each live peer's RPC port and
// records it as that peer's Latency. This is a real measurement, not a
// placeholder: the dial's handshake is one round trip over the same link
// the RPC traffic itself would use. A peer that fails to answer just keeps
// its last-known latency (or zero, if never probed) -- water-fill treats
// that as "no measured cost yet" rather than excluding the peer.
func probeLoop(ctx context.Context, t *Table) {
	ticker := time.NewTicker(probeInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, p := range t.Peers() {
				if p.RPCPort == 0 {
					continue
				}
				addr := net.JoinHostPort(p.Addr, strconv.Itoa(p.RPCPort))
				start := time.Now()
				conn, err := net.DialTimeout("tcp", addr, probeTimeout)
				if err != nil {
					slog.Debug("cluster: latency probe failed, keeping last-known value", "id", p.ID, "addr", addr, "error", err)
					continue
				}
				conn.Close()
				latency := time.Since(start)
				slog.Debug("cluster: probed peer latency", "id", p.ID, "addr", addr, "latency", latency)
				t.setLatency(p.ID, latency)
			}
		}
	}
}

// listenLoop reads incoming beacons and updates the table, ignoring our own.
func listenLoop(ctx context.Context, conn *net.UDPConn, cfg Config, t *Table) {
	buf := make([]byte, 64*1024)
	for {
		n, src, err := conn.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				slog.Debug("cluster: read udp", "error", err)
				return
			}
		}
		var a announcement
		if err := json.Unmarshal(buf[:n], &a); err != nil {
			slog.Debug("cluster: decode announcement", "error", err, "src", src)
			continue
		}
		slog.Debug("cluster: received announcement", "id", a.ID, "self_id", cfg.SelfID, "src", src)
		if a.ID == "" || a.ID == cfg.SelfID {
			continue
		}
		t.observe(Peer{
			ID:         a.ID,
			Addr:       src.IP.String(),
			RPCPort:    a.RPCPort,
			Devices:    a.Devices,
			ProtoMajor: a.ProtoMajor,
			ProtoMinor: a.ProtoMinor,
			Load:       a.Load,
			LastSeen:   time.Now(),
		})
	}
}
