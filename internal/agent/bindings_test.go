package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testBindings(t *testing.T) *Bindings {
	t.Helper()
	root := t.TempDir()
	s := NewBindings()
	s.Dir = filepath.Join(root, "private")
	s.Root = filepath.Join(root, "shares")
	s.Units = filepath.Join(root, "units")
	s.Containers = filepath.Join(root, "containers")
	s.Simulation = true
	for _, p := range []string{s.Units, s.Containers} {
		if err := os.MkdirAll(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	return s
}
func TestBindingsScopeAndSecrets(t *testing.T) {
	s := testBindings(t)
	ctx := context.Background()
	in := BindingInput{ID: "photos", Role: "photos", Server: "nas.lan", Share: "Photos", Username: "immich", Password: "private%pass,word"}
	if err := s.Create(ctx, in); err != nil {
		t.Fatal(err)
	}
	view, err := s.List(ctx)
	if err != nil || len(view) != 1 {
		t.Fatalf("%+v %v", view, err)
	}
	data, _ := json.Marshal(view)
	if strings.Contains(string(data), in.Password) || strings.Contains(string(data), in.Username) {
		t.Fatal("secret escaped")
	}
	cred := filepath.Join(s.Dir, "photos.credentials")
	info, _ := os.Stat(cred)
	if info.Mode().Perm() != 0600 {
		t.Fatal("credentials permissions")
	}
	if err = s.Create(ctx, in); err == nil {
		t.Fatal("replaced existing binding")
	}
	conf := filepath.Join(s.Containers, "170.conf")
	os.WriteFile(conf, []byte("mp0: "+view[0].Path+",mp=/data/photos\n"), 0600)
	if err = s.Delete(ctx, "photos"); err == nil {
		t.Fatal("deleted used binding")
	}
	if _, err = os.Stat(cred); err != nil {
		t.Fatal("removed credentials of used binding")
	}
	os.Remove(conf)
	if err = s.Delete(ctx, "photos"); err != nil {
		t.Fatal(err)
	}
}
func TestBindingsRejectUnsafeInput(t *testing.T) {
	good := BindingInput{ID: "photos", Role: "photos", Server: "nas", Share: "Photos", Username: "app", Password: "pass"}
	for _, change := range []func(*BindingInput){func(b *BindingInput) { b.ID = "../bad" }, func(b *BindingInput) { b.Share = "Photos,vers=1" }, func(b *BindingInput) { b.Server = "nas%h" }, func(b *BindingInput) { b.Password = "pass\nusername=root" }, func(b *BindingInput) { b.Role = "unknown" }} {
		b := good
		change(&b)
		if b.Validate() == nil {
			t.Fatalf("accepted %+v", b)
		}
	}
}
func TestBindingFailedMountCleansOnlyOwnFiles(t *testing.T) {
	s := testBindings(t)
	s.Simulation = false
	s.output = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name == "findmnt" {
			return []byte("ext4 /dev/root"), nil
		}
		return nil, nil
	}
	if err := s.Create(context.Background(), BindingInput{ID: "photos", Role: "photos", Server: "nas", Share: "Photos", Username: "app", Password: "pass"}); err == nil {
		t.Fatal("accepted wrong filesystem")
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "photos.json")); !os.IsNotExist(err) {
		t.Fatal("published failed mount")
	}
}
