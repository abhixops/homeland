package icons

import "testing"

func TestCleanIconName(t *testing.T) {
	tests := []struct {
		in   string
		want string
		ok   bool
	}{
		{"Jellyfin", "jellyfin", true},
		{"pi-hole.svg", "pi-hole", true},
		{"jellystat.PNG", "jellystat", true},
		{"", "", false},
		{"../../etc/passwd", "", false},
		{"sub/dir", "", false},
		{`..\win`, "", false},
		{".hidden", "", false},
		{"c:evil", "", false},
	}
	for _, tt := range tests {
		got, ok := cleanIconName(tt.in)
		if got != tt.want || ok != tt.ok {
			t.Errorf("cleanIconName(%q) = (%q, %v), want (%q, %v)", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

func TestGetIconPathRejectsTraversal(t *testing.T) {
	f := NewFetcher(t.TempDir())
	if got := f.GetIconPath("../../evil"); got != "/static/icons/default.svg" {
		t.Fatalf("GetIconPath = %q, want default icon", got)
	}
}
