package docker

import (
	"testing"

	"github.com/abhixops/homeland/internal/config"
)

func TestMergeWithConfigOrderIsStable(t *testing.T) {
	yaml := []config.Group{{Name: "Media", Apps: []config.App{{Name: "Jellyfin"}}}}
	discovered := map[string][]config.App{
		"Zeta":  {{Name: "z"}},
		"Alpha": {{Name: "a"}},
		"Media": {{Name: "Jellyfin"}, {Name: "Radarr"}}, // Jellyfin duplicates YAML
		"Mid":   {{Name: "m"}},
	}

	for range 20 { // map order is random; repeat to catch instability
		got := MergeWithConfig(yaml, discovered)
		var names []string
		for _, g := range got {
			names = append(names, g.Name)
		}
		want := []string{"Media", "Alpha", "Mid", "Zeta"}
		if len(names) != len(want) {
			t.Fatalf("groups = %v, want %v", names, want)
		}
		for i := range want {
			if names[i] != want[i] {
				t.Fatalf("groups = %v, want %v", names, want)
			}
		}
		if n := len(got[0].Apps); n != 2 {
			t.Fatalf("Media has %d apps, want 2 (YAML Jellyfin + docker Radarr)", n)
		}
	}
}

func TestSignatureIgnoresOrder(t *testing.T) {
	a := map[string][]config.App{"G": {{Name: "x"}, {Name: "y"}}, "H": {{Name: "z"}}}
	b := map[string][]config.App{"H": {{Name: "z"}}, "G": {{Name: "y"}, {Name: "x"}}}
	if signature(a) != signature(b) {
		t.Fatal("signature should not depend on order")
	}
	b["H"][0].URL = "http://changed"
	if signature(a) == signature(b) {
		t.Fatal("signature should change when an app's URL changes")
	}
}
