package cluster

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/ollama/ollama/ml"
)

// TestAnnouncementJSONRoundTrip covers the wire format without touching the
// network: encode an announcement the way broadcastLoop does, decode it the
// way listenLoop does, and check every field survives.
func TestAnnouncementJSONRoundTrip(t *testing.T) {
	want := announcement{
		ID:      "peer-1",
		RPCPort: 50052,
		Devices: []ml.DeviceInfo{
			{DeviceID: ml.DeviceID{ID: "0", Library: "CUDA"}, Name: "A4500", TotalMemory: 20 << 30, FreeMemory: 12 << 30},
		},
		ProtoMajor: RPCProtoMajor,
		ProtoMinor: RPCProtoMinor,
		Load:       0.42,
	}

	buf, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got announcement
	if err := json.Unmarshal(buf, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.ID != want.ID {
		t.Fatalf("ID: got %q want %q", got.ID, want.ID)
	}
	if got.RPCPort != want.RPCPort {
		t.Fatalf("RPCPort: got %d want %d", got.RPCPort, want.RPCPort)
	}
	if got.ProtoMajor != want.ProtoMajor || got.ProtoMinor != want.ProtoMinor {
		t.Fatalf("proto: got %d.%d want %d.%d", got.ProtoMajor, got.ProtoMinor, want.ProtoMajor, want.ProtoMinor)
	}
	if got.Load != want.Load {
		t.Fatalf("Load: got %v want %v", got.Load, want.Load)
	}
	if len(got.Devices) != 1 || got.Devices[0].Name != "A4500" || got.Devices[0].FreeMemory != 12<<30 {
		t.Fatalf("Devices: got %+v", got.Devices)
	}
}

// TestTablePeersExpiry checks lazy expiry: a peer heard long enough ago
// (older than TTL) must not show up in Peers(), a fresh one must.
func TestTablePeersExpiry(t *testing.T) {
	tbl := &Table{
		peers: make(map[string]Peer),
		ttl:   time.Minute,
	}
	tbl.observe(Peer{ID: "stale", LastSeen: time.Now().Add(-time.Hour)})
	tbl.observe(Peer{ID: "fresh", LastSeen: time.Now()})

	peers := tbl.Peers()
	if len(peers) != 1 || peers[0].ID != "fresh" {
		t.Fatalf("Peers() = %+v, want only \"fresh\"", peers)
	}
}

// TestTableSelf checks Self() reflects the most recent updateSelf call.
func TestTableSelf(t *testing.T) {
	tbl := &Table{peers: make(map[string]Peer)}
	tbl.updateSelf(Peer{ID: "me", RPCPort: 1234})
	if got := tbl.Self(); got.ID != "me" || got.RPCPort != 1234 {
		t.Fatalf("Self() = %+v", got)
	}
}

// TestListenLoopReceivesAnnouncement is the one real-networking test: it
// exercises Start's actual listening socket over loopback UDP unicast
// rather than OS broadcast, since a broadcast to 255.255.255.255 typically
// isn't delivered back to a 127.0.0.1 listener (and there's no second host
// to broadcast to in this sandbox). A hard context timeout means a genuine
// networking problem here fails the test instead of hanging.
func TestListenLoopReceivesAnnouncement(t *testing.T) {
	// Grab a free UDP port from the OS, then release it for Start to reuse.
	probe, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Skipf("no loopback UDP available in this sandbox: %v", err)
	}
	port := probe.LocalAddr().(*net.UDPAddr).Port
	probe.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cfg := Config{
		Port:        port,
		Interval:    time.Hour, // don't let our own broadcastLoop interfere
		TTL:         time.Minute,
		SelfID:      "self",
		SelfDevices: func() []ml.DeviceInfo { return nil },
		SelfRPCPort: func() int { return 0 },
		SelfLoad:    func() float64 { return 0 },
	}
	tbl, err := Start(ctx, cfg)
	if err != nil {
		t.Skipf("could not start discovery listener in this sandbox: %v", err)
	}

	sender, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port})
	if err != nil {
		t.Skipf("no loopback UDP send available in this sandbox: %v", err)
	}
	defer sender.Close()

	buf, err := json.Marshal(announcement{
		ID:         "peer-2",
		RPCPort:    50052,
		ProtoMajor: RPCProtoMajor,
		ProtoMinor: RPCProtoMinor,
		Load:       0.1,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	deadline := time.Now().Add(4 * time.Second)
	for {
		if _, err := sender.Write(buf); err != nil {
			t.Fatalf("send: %v", err)
		}
		for _, p := range tbl.Peers() {
			if p.ID == "peer-2" {
				return // found it, test passes
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("peer-2 never appeared in the table within the timeout")
		}
		select {
		case <-ctx.Done():
			t.Fatal("context timed out waiting for peer-2")
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func TestResolveSeeds(t *testing.T) {
	addrs := resolveSeeds([]string{"127.0.0.1:11435", "not a valid seed:::", "192.168.1.42:50999"})
	if len(addrs) != 2 {
		t.Fatalf("expected 2 resolved seeds (1 invalid skipped), got %d: %v", len(addrs), addrs)
	}
	if addrs[0].String() != "127.0.0.1:11435" {
		t.Errorf("addrs[0] = %v, want 127.0.0.1:11435", addrs[0])
	}
	if addrs[1].String() != "192.168.1.42:50999" {
		t.Errorf("addrs[1] = %v, want 192.168.1.42:50999", addrs[1])
	}
}

func TestResolveSeedsEmpty(t *testing.T) {
	if addrs := resolveSeeds(nil); len(addrs) != 0 {
		t.Errorf("resolveSeeds(nil) = %v, want empty", addrs)
	}
}
