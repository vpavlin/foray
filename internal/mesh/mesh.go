// Package mesh is what Foray needs from the shrooms daemon: this machine's
// address on each mesh, to serve on, and the names of the devices that call.
//
// The same answers shrooms-agent gets, the same way — over the daemon's
// control socket — so a caller is named here exactly as it is named there.
package mesh

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"sync"
	"time"
)

// DefaultSocket is the shrooms daemon's control socket.
const DefaultSocket = "/run/shrooms/shrooms.sock"

// Status is the part of the daemon's /status Foray reads.
type Status struct {
	Name   string `json:"name"`
	Meshes []struct {
		Label   string `json:"label"`
		Overlay string `json:"overlay"`
	} `json:"meshes"`
	Peers []struct {
		Name      string `json:"name"`
		Mesh      string `json:"mesh"`
		Overlay   string `json:"overlay"`
		OverlayV4 string `json:"overlay_v4"`
	} `json:"peers"`
}

// Fetch asks the daemon.
func Fetch(sock string) (Status, error) {
	c := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", sock)
		},
	}}
	resp, err := c.Get("http://unix/status")
	if err != nil {
		return Status{}, fmt.Errorf("the shrooms daemon (%s): %w", sock, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Status{}, fmt.Errorf("the shrooms daemon: %s", resp.Status)
	}
	var st Status
	return st, json.NewDecoder(resp.Body).Decode(&st)
}

// Addresses is this machine's overlay address on each mesh, by label.
func (st Status) Addresses() map[string]netip.Addr {
	out := map[string]netip.Addr{}
	for _, m := range st.Meshes {
		if a, err := netip.ParseAddr(m.Overlay); err == nil {
			out[m.Label] = a
		}
	}
	return out
}

// Names maps a caller's address to its name on the mesh, "phone.home",
// refreshed from the daemon so a device that joins later is named too.
type Names struct {
	mu   sync.Mutex
	by   map[netip.Addr]string
	self string
}

// Update takes the daemon's latest roster.
func (n *Names) Update(st Status) {
	by := map[netip.Addr]string{}
	for _, p := range st.Peers {
		name := p.Name
		if p.Mesh != "" {
			name += "." + p.Mesh
		}
		for _, s := range []string{p.Overlay, p.OverlayV4} {
			if a, err := netip.ParseAddr(s); err == nil {
				by[a.Unmap()] = name
			}
		}
	}
	// This machine, calling from one of its own mesh addresses, is itself.
	for _, m := range st.Meshes {
		if a, err := netip.ParseAddr(m.Overlay); err == nil && st.Name != "" {
			by[a.Unmap()] = st.Name
		}
	}
	n.mu.Lock()
	n.by, n.self = by, st.Name
	n.mu.Unlock()
}

// Who names an address, or "" for one the mesh does not know.
func (n *Names) Who(a netip.Addr) string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.by[a.Unmap()]
}

// Self is this machine's name on the mesh.
func (n *Names) Self() string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.self
}
