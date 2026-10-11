package mesh

import (
	"encoding/json"
	"net/netip"
	"testing"
)

func TestNamesKnowPeersAndThisMachine(t *testing.T) {
	var st Status
	json.Unmarshal([]byte(`{"name":"jimmy-crib",
		"meshes":[{"label":"home","overlay":"fd7b::1"}],
		"peers":[{"name":"phone","mesh":"home","overlay":"fd7b::2","overlay_v4":"198.18.0.2"},
		         {"name":"old","overlay":"fd7b::3"}]}`), &st)
	var n Names
	n.Update(st)
	for addr, want := range map[string]string{"fd7b::2": "phone.home", "198.18.0.2": "phone.home",
		"fd7b::3": "old", "fd7b::1": "jimmy-crib", "fd7b::9": ""} {
		if got := n.Who(netip.MustParseAddr(addr)); got != want {
			t.Errorf("%s: %q, want %q", addr, got, want)
		}
	}
	if a := st.Addresses()["home"]; a.String() != "fd7b::1" {
		t.Errorf("address on home: %v", a)
	}
}
