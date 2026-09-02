// Package awg runs `awg show <iface> dump` and parses its tab-separated
// output into a machine-friendly form.
package awg

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Peer is one peer line from `awg show <iface> dump`.
type Peer struct {
	PublicKey       string
	Endpoint        string // "" if the peer has never connected
	AllowedIPs      []string
	HasHandshake    bool // false if latest-handshake is 0 (never connected)
	LatestHandshake time.Time
	RxBytes         uint64
	TxBytes         uint64
	KeepaliveSec    int
}

// Show runs `<bin> show <iface> dump` and parses its output.
func Show(ctx context.Context, bin, iface string) ([]Peer, error) {
	cmd := exec.CommandContext(ctx, bin, "show", iface, "dump")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s show %s dump: %w: %s", bin, iface, err, strings.TrimSpace(stderr.String()))
	}

	return ParseDump(stdout.Bytes())
}

// ParseDump parses the tab-separated output of `awg show <iface> dump`.
//
// Line 0 describes the interface itself (private-key, public-key,
// listen-port, fwmark) and is skipped. Every following line describes one
// peer: public-key, preshared-key, endpoint, allowed-ips,
// latest-handshake (unix seconds), rx-bytes, tx-bytes, persistent-keepalive.
func ParseDump(out []byte) ([]Peer, error) {
	text := strings.TrimRight(string(out), "\n")
	if text == "" {
		return nil, fmt.Errorf("empty dump output")
	}
	lines := strings.Split(text, "\n")

	peers := make([]Peer, 0, len(lines))
	for i, line := range lines {
		if i == 0 || line == "" {
			continue // interface line, or a stray blank line
		}

		fields := strings.Split(line, "\t")
		if len(fields) != 8 {
			return nil, fmt.Errorf("dump line %d: expected 8 fields, got %d", i+1, len(fields))
		}

		p := Peer{PublicKey: fields[0]}

		if fields[2] != "" && fields[2] != "(none)" {
			p.Endpoint = fields[2]
		}
		if fields[3] != "" && fields[3] != "(none)" {
			p.AllowedIPs = strings.Split(fields[3], ",")
		}

		handshake, err := strconv.ParseInt(fields[4], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("dump line %d: bad latest-handshake %q: %w", i+1, fields[4], err)
		}
		if handshake > 0 {
			p.HasHandshake = true
			p.LatestHandshake = time.Unix(handshake, 0)
		}

		rx, err := strconv.ParseUint(fields[5], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("dump line %d: bad rx-bytes %q: %w", i+1, fields[5], err)
		}
		p.RxBytes = rx

		tx, err := strconv.ParseUint(fields[6], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("dump line %d: bad tx-bytes %q: %w", i+1, fields[6], err)
		}
		p.TxBytes = tx

		keepalive, err := strconv.Atoi(fields[7])
		if err != nil {
			return nil, fmt.Errorf("dump line %d: bad persistent-keepalive %q: %w", i+1, fields[7], err)
		}
		p.KeepaliveSec = keepalive

		peers = append(peers, p)
	}

	return peers, nil
}
