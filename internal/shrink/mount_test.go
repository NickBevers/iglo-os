package shrink

import "testing"

func TestTopMountIsCIFS(t *testing.T) {
	for out, want := range map[string]bool{
		"cifs\n":        true,
		"cifs\ncifs\n":  true,
		"ext4\ncifs\n":  true,
		"cifs\ntmpfs\n": false,
		"":              false,
		"nfs4\n":        false,
		"  cifs  \n\n":  true,
	} {
		if got := topMountIsCIFS(out); got != want {
			t.Errorf("topMountIsCIFS(%q) = %v, want %v", out, got, want)
		}
	}
}
