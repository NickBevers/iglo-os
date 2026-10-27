package agent

// Scoped SMB mounts live on the host. Credentials never enter a guest or a
// manager export. A binding is immutable; removing a used binding is refused.
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
)

var bindingID = regexp.MustCompile(`^[a-z][a-z0-9]{0,31}$`)
var BindingRoles = []string{"videos", "music", "photos", "games", "inbox", "cameras", "backups"}

type BindingInput struct {
	ID       string `json:"id"`
	Role     string `json:"role"`
	Server   string `json:"server"`
	Share    string `json:"share"`
	Username string `json:"username"`
	Password string `json:"password,omitempty"`
	ReadOnly bool   `json:"readOnly"`
}

type Binding struct {
	ID        string   `json:"id"`
	Role      string   `json:"role"`
	Source    string   `json:"source"`
	Path      string   `json:"path"`
	ReadOnly  bool     `json:"readOnly"`
	Mounted   bool     `json:"mounted"`
	Consumers []string `json:"consumers"`
	// ProxmoxStorage is the Proxmox storage that keeps its backups on this
	// share, like iglo-backups. Empty when there is none.
	ProxmoxStorage string `json:"proxmoxStorage,omitempty"`
}

func (b BindingInput) Validate() error {
	if !bindingID.MatchString(b.ID) || !slices.Contains(BindingRoles, b.Role) ||
		!hostPattern.MatchString(b.Server) || !smbSharePattern.MatchString(b.Share) ||
		!smbUserPattern.MatchString(b.Username) || len(b.Password) == 0 || len(b.Password) > 256 || strings.ContainsFunc(b.Password, unicode.IsControl) {
		return fmt.Errorf("%w: use a lowercase alphanumeric ID, a folder role, NAS hostname, share name and SMB credentials without line breaks", ErrInvalidAnswers)
	}
	return nil
}

type Bindings struct {
	Dir, Root, Units, Containers string
	// StorageConfig is the storage.cfg of Proxmox. Empty means /etc/pve/storage.cfg.
	StorageConfig string
	// Simulation is used only by the local development fixture.
	Simulation bool
	output     func(context.Context, string, ...string) ([]byte, error)
	mu         sync.Mutex
}

func NewBindings() *Bindings {
	return &Bindings{Dir: "/etc/iglo/storage", Root: "/mnt/iglo/shares", Units: "/etc/systemd/system", Containers: "/etc/pve/lxc", output: commandOutput}
}

func (s *Bindings) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if s.Simulation {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	return s.output(ctx, name, args...)
}

func (s *Bindings) consumers(path string) ([]string, error) {
	files, err := filepath.Glob(filepath.Join(s.Containers, "*.conf"))
	if err != nil {
		return nil, err
	}
	result := []string{}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		for line := range strings.Lines(string(data)) {
			key, value, ok := strings.Cut(line, ":")
			if !ok || !regexp.MustCompile(`^mp[0-9]+$`).MatchString(key) {
				continue
			}
			if source, _, _ := strings.Cut(strings.TrimSpace(value), ","); source == path || strings.HasPrefix(source, path+"/") {
				result = append(result, strings.TrimSuffix(filepath.Base(file), ".conf"))
				break
			}
		}
	}
	return result, nil
}

func (s *Bindings) List(ctx context.Context) ([]Binding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	files, err := filepath.Glob(filepath.Join(s.Dir, "*.json"))
	if err != nil {
		return nil, err
	}
	result := []Binding{}
	storages := s.proxmoxStorages()
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		var b Binding
		if err = json.Unmarshal(data, &b); err != nil {
			return nil, err
		}
		b.Consumers, err = s.consumers(b.Path)
		if err != nil {
			return nil, err
		}
		b.ProxmoxStorage = storages[filepath.Clean(b.Path)]
		if s.Simulation {
			b.Mounted = true
		} else {
			out, err := s.run(ctx, "findmnt", "--mountpoint", b.Path, "--noheadings", "--output", "FSTYPE,SOURCE")
			f := strings.Fields(string(out))
			b.Mounted = err == nil && len(f) == 2 && f[0] == "cifs" && f[1] == b.Source
		}
		result = append(result, b)
	}
	return result, nil
}

func (s *Bindings) Create(ctx context.Context, in BindingInput) error {
	if err := in.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	path := filepath.Join(s.Root, in.ID)
	unit := "mnt-iglo-shares-" + in.ID + ".mount"
	if _, err := os.Stat(filepath.Join(s.Dir, in.ID+".json")); !errors.Is(err, os.ErrNotExist) {
		return errors.New("this binding already exists")
	}
	// Do not take over an existing host mount, directory, unit or credential file.
	for _, name := range []string{path, filepath.Join(s.Units, unit), filepath.Join(s.Dir, in.ID+".credentials"), filepath.Join(s.Dir, in.ID+".conf")} {
		if _, err := os.Lstat(name); !errors.Is(err, os.ErrNotExist) {
			return errors.New("a host path for this binding already exists; choose another ID")
		}
	}
	if err := os.MkdirAll(s.Dir, 0700); err != nil {
		return err
	}
	if err := os.MkdirAll(s.Root, 0755); err != nil {
		return err
	}
	// An unprivileged container cannot write into the underlying directory if
	// the NAS is absent. A systemd dependency also mounts the share before boot.
	if err := os.Mkdir(path, 0000); err != nil {
		return err
	}
	cred := filepath.Join(s.Dir, in.ID+".credentials")
	ok := false
	defer func() {
		if !ok {
			// If stop fails, keep the private credentials and manifest for recovery.
			if _, err := s.run(context.Background(), "systemctl", "disable", "--now", unit); err != nil {
				return
			}
			for _, f := range []string{cred, filepath.Join(s.Dir, in.ID+".conf"), filepath.Join(s.Units, unit)} {
				os.Remove(f)
			}
			os.Remove(path)
			s.run(context.Background(), "systemctl", "daemon-reload")
		}
	}()
	if err := os.WriteFile(cred, []byte("username="+in.Username+"\npassword="+in.Password+"\n"), 0600); err != nil {
		return err
	}
	mode := "rw"
	if in.ReadOnly {
		mode = "ro"
	}
	text := fmt.Sprintf("[Unit]\nDescription=Iglo scoped %s share\nWants=network-online.target\nAfter=network-online.target\nBefore=pve-guests.service\n[Mount]\nWhat=//%s/%s\nWhere=%s\nType=cifs\nOptions=credentials=%s,vers=3.0,uid=100000,gid=101000,file_mode=0664,dir_mode=0775,nosuid,nodev,noexec,_netdev,%s\nTimeoutSec=20\n[Install]\nWantedBy=multi-user.target\n", in.Role, in.Server, in.Share, path, cred, mode)
	if err := os.WriteFile(filepath.Join(s.Units, unit), []byte(text), 0644); err != nil {
		return err
	}
	if _, err := s.run(ctx, "systemctl", "daemon-reload"); err != nil {
		return errors.New("could not reload mount units")
	}
	if _, err := s.run(ctx, "systemctl", "enable", "--now", unit); err != nil {
		return errors.New("could not mount the SMB share; check NAS access and credentials")
	}
	if !s.Simulation {
		out, err := s.run(ctx, "findmnt", "--mountpoint", path, "--noheadings", "--output", "FSTYPE,SOURCE")
		f := strings.Fields(string(out))
		if err != nil || len(f) != 2 || f[0] != "cifs" || f[1] != "//"+in.Server+"/"+in.Share {
			return errors.New("mounted source does not match this SMB binding")
		}
	}
	manifest := fmt.Sprintf("ROLE=%s\nREAD_ONLY=%t\nSOURCE=//%s/%s\n", in.Role, in.ReadOnly, in.Server, in.Share)
	if err := os.WriteFile(filepath.Join(s.Dir, in.ID+".conf"), []byte(manifest), 0600); err != nil {
		return err
	}
	b := Binding{ID: in.ID, Role: in.Role, Source: "//" + in.Server + "/" + in.Share, Path: path, ReadOnly: in.ReadOnly}
	data, _ := json.Marshal(b)
	if err := os.WriteFile(filepath.Join(s.Dir, in.ID+".json"), data, 0600); err != nil {
		return err
	}
	ok = true
	return nil
}

func (s *Bindings) Delete(ctx context.Context, id string) error {
	if !bindingID.MatchString(id) {
		return ErrInvalidAnswers
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := os.Stat(filepath.Join(s.Dir, id+".json")); err != nil {
		return errors.New("binding does not exist")
	}
	used, err := s.consumers(filepath.Join(s.Root, id))
	if err != nil {
		return err
	}
	if len(used) > 0 {
		return errors.New("remove the binding from its containers first")
	}
	if storage := s.proxmoxStorages()[filepath.Join(s.Root, id)]; storage != "" {
		return fmt.Errorf("Proxmox keeps backups on this share as the storage %s; stop that first", storage)
	}
	unit := "mnt-iglo-shares-" + id + ".mount"
	if _, err = s.run(ctx, "systemctl", "disable", "--now", unit); err != nil {
		return errors.New("could not stop the mount; no files were removed")
	}
	for _, f := range []string{filepath.Join(s.Units, unit), filepath.Join(s.Dir, id+".conf"), filepath.Join(s.Dir, id+".credentials"), filepath.Join(s.Dir, id+".json")} {
		if err = os.Remove(f); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	// Never recurse into a share. Only remove its empty host mount point.
	if err = os.Remove(filepath.Join(s.Root, id)); err != nil {
		return err
	}
	_, err = s.run(ctx, "systemctl", "daemon-reload")
	return err
}

// Register serves only on the trusted host-agent Unix socket. The manager adds
// session authentication and CSRF checks to its corresponding routes.
func (s *Bindings) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/storage/bindings", func(w http.ResponseWriter, r *http.Request) {
		list, err := s.List(r.Context())
		if err != nil {
			http.Error(w, "could not read bindings", 502)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct {
			Bindings []Binding `json:"bindings"`
			Sample   bool      `json:"sample"`
		}{list, s.Simulation})
	})
	mux.HandleFunc("POST /v1/storage/bindings", func(w http.ResponseWriter, r *http.Request) {
		var in BindingInput
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in) != nil {
			http.Error(w, "invalid request", 400)
			return
		}
		if err := s.Create(r.Context(), in); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		w.WriteHeader(204)
	})
	s.registerProxmoxBackups(mux)
	mux.HandleFunc("DELETE /v1/storage/bindings/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Delete(r.Context(), r.PathValue("id")); err != nil {
			http.Error(w, err.Error(), 409)
			return
		}
		w.WriteHeader(204)
	})
}
