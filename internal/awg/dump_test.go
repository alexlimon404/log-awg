package awg

import (
	"testing"
	"time"
)

func TestParseDump(t *testing.T) {
	// Interface line + three peers: one active with traffic, one that
	// completed a handshake but is currently idle (no endpoint reported),
	// and one that has never connected (matches `938ozr...`/`TXlMv...`
	// shapes seen in real `awg show awg0 dump` output).
	dump := "" +
		"cHJpdmtleQ==\tcHVia2V5\t9440\t0\n" +
		"938ozrqky5CZVl3msIieRPneqnPyLmzjp4h4D1KA5UI=\t(none)\t46.138.180.183:1094\t10.12.0.3/32\t1756800000\t1524000000\t31424000000\toff\n" +
		"focNGHcMYY156toc7Yr24p4T4XQ3cxn70bcM12qRehs=\t(none)\t(none)\t10.12.0.5/32\t1756798000\t21561344\t442306560\t25\n" +
		"TXlMvhMfx1y4jxz5FoE2H8+jh5dRLNJ0PLq9r2YgR3o=\t(none)\t(none)\t10.12.0.4/32\t0\t0\t0\t0\n"

	peers, err := ParseDump([]byte(dump))
	if err != nil {
		t.Fatalf("ParseDump: %v", err)
	}
	if len(peers) != 3 {
		t.Fatalf("expected 3 peers, got %d", len(peers))
	}

	active := peers[0]
	if active.PublicKey != "938ozrqky5CZVl3msIieRPneqnPyLmzjp4h4D1KA5UI=" {
		t.Errorf("unexpected public key: %s", active.PublicKey)
	}
	if active.Endpoint != "46.138.180.183:1094" {
		t.Errorf("unexpected endpoint: %q", active.Endpoint)
	}
	if !active.HasHandshake {
		t.Error("expected HasHandshake=true")
	}
	if !active.LatestHandshake.Equal(time.Unix(1756800000, 0)) {
		t.Errorf("unexpected latest handshake: %v", active.LatestHandshake)
	}
	if active.RxBytes != 1524000000 || active.TxBytes != 31424000000 {
		t.Errorf("unexpected transfer: rx=%d tx=%d", active.RxBytes, active.TxBytes)
	}
	if len(active.AllowedIPs) != 1 || active.AllowedIPs[0] != "10.12.0.3/32" {
		t.Errorf("unexpected allowed ips: %v", active.AllowedIPs)
	}
	if active.KeepaliveSec != 0 {
		t.Errorf("expected KeepaliveSec=0 for \"off\", got %d", active.KeepaliveSec)
	}

	idle := peers[1]
	if idle.Endpoint != "" {
		t.Errorf("expected empty endpoint for (none), got %q", idle.Endpoint)
	}
	if !idle.HasHandshake {
		t.Error("expected HasHandshake=true for previously-connected idle peer")
	}
	if idle.KeepaliveSec != 25 {
		t.Errorf("unexpected keepalive: %d", idle.KeepaliveSec)
	}

	neverConnected := peers[2]
	if neverConnected.HasHandshake {
		t.Error("expected HasHandshake=false for a peer with latest-handshake=0")
	}
	if !neverConnected.LatestHandshake.IsZero() {
		t.Errorf("expected zero LatestHandshake, got %v", neverConnected.LatestHandshake)
	}
}

func TestParseDumpEmpty(t *testing.T) {
	if _, err := ParseDump([]byte("")); err == nil {
		t.Fatal("expected error on empty output")
	}
}

func TestParseDumpNoPeers(t *testing.T) {
	peers, err := ParseDump([]byte("cHJpdmtleQ==\tcHVia2V5\t9440\t0\n"))
	if err != nil {
		t.Fatalf("ParseDump: %v", err)
	}
	if len(peers) != 0 {
		t.Fatalf("expected 0 peers, got %d", len(peers))
	}
}

func TestParseDumpBadLine(t *testing.T) {
	dump := "cHJpdmtleQ==\tcHVia2V5\t9440\t0\n" +
		"badline\twith\ttoo\tfew\tfields\n"
	if _, err := ParseDump([]byte(dump)); err == nil {
		t.Fatal("expected error on malformed peer line")
	}
}
