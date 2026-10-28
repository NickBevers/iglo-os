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
func TestBindingsListReadsMountsOnce(t *testing.T) {
	s := testBindings(t)
	s.Simulation = false
	videos, photos := filepath.Join(s.Root, "videos"), filepath.Join(s.Root, "photos")
	findmnt := 0
	s.output = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "findmnt" {
			t.Fatalf("ran %s", name)
		}
		findmnt++
		return []byte(videos + " //nas/Videos\n" + photos + " //other/Photos\n"), nil
	}
	if err := os.MkdirAll(s.Dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, b := range []Binding{
		{ID: "videos", Role: "videos", Source: "//nas/Videos", Path: videos},
		{ID: "photos", Role: "photos", Source: "//nas/Photos", Path: photos},
		{ID: "music", Role: "music", Source: "//nas/Music", Path: filepath.Join(s.Root, "music")},
	} {
		data, _ := json.Marshal(b)
		os.WriteFile(filepath.Join(s.Dir, b.ID+".json"), data, 0600)
	}
	os.WriteFile(filepath.Join(s.Containers, "120.conf"), []byte("mp0: "+videos+"/movies,mp=/data/media/movies\nmp1: "+videos+"x,mp=/x\n"), 0600)
	os.WriteFile(filepath.Join(s.Containers, "110.conf"), []byte("arch: amd64\nmp0: "+videos+",mp=/data\n"), 0600)

	list, err := s.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if findmnt != 1 {
		t.Errorf("findmnt ran %d times", findmnt)
	}
	got := map[string]Binding{}
	for _, b := range list {
		got[b.ID] = b
	}
	if !got["videos"].Mounted || got["photos"].Mounted || got["music"].Mounted {
		t.Errorf("mounted: %+v", list)
	}
	if strings.Join(got["videos"].Consumers, ",") != "110,120" || len(got["photos"].Consumers) != 0 {
		t.Errorf("consumers: %+v", list)
	}
}
