package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// A Backups share can hold the backups of Proxmox. The agent registers the
// share's own mount as a Proxmox storage of type dir, so the SMB password
// stays with Iglo-OS and Proxmox gets no copy. The storage has
// is_mountpoint, so Proxmox refuses to back up when the share isn't mounted.

// proxmoxStorageConfig is where Proxmox keeps its storages.
const proxmoxStorageConfig = "/etc/pve/storage.cfg"

// ProxmoxBackupStorage is the Proxmox storage of a Backups share.
func ProxmoxBackupStorage(id string) string { return "iglo-" + id }

// dirStorages maps the path of each dir storage in storage.cfg to its ID.
func dirStorages(config string) map[string]string {
	storages := map[string]string{}
	id := ""
	for line := range strings.Lines(config) {
		if kind, name, ok := strings.Cut(strings.TrimSpace(line), ": "); ok && !strings.HasPrefix(line, "\t") && !strings.HasPrefix(line, " ") {
			id = ""
			if kind == "dir" {
				id = strings.TrimSpace(name)
			}
			continue
		}
		if key, value, ok := strings.Cut(strings.TrimSpace(line), " "); ok && key == "path" && id != "" {
			storages[filepath.Clean(strings.TrimSpace(value))] = id
		}
	}
	return storages
}

func (s *Bindings) storageConfig() string {
	if s.StorageConfig != "" {
		return s.StorageConfig
	}
	return proxmoxStorageConfig
}

// proxmoxStorages reads the dir storages of Proxmox. A host without the file has none.
func (s *Bindings) proxmoxStorages() map[string]string {
	data, _ := os.ReadFile(s.storageConfig())
	return dirStorages(string(data))
}

// UseForProxmoxBackups turns the Proxmox backups on a Backups share on or off.
// Off only removes the storage from Proxmox; the backups on the NAS stay.
func (s *Bindings) UseForProxmoxBackups(ctx context.Context, id string, on bool) error {
	if !bindingID.MatchString(id) {
		return ErrInvalidAnswers
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(filepath.Join(s.Dir, id+".json"))
	if err != nil {
		return errors.New("binding does not exist")
	}
	var binding Binding
	if err := json.Unmarshal(data, &binding); err != nil {
		return err
	}
	storage := ProxmoxBackupStorage(id)
	current := s.proxmoxStorages()[filepath.Clean(binding.Path)]

	if on {
		switch {
		case binding.Role != "backups" || binding.ReadOnly:
			return errors.New("only a Backups share with write access can hold Proxmox backups")
		case current == storage:
			return nil
		case current != "":
			return fmt.Errorf("Proxmox already uses this share as the storage %s", current)
		}
		if !s.Simulation {
			out, err := s.run(ctx, "findmnt", "--mountpoint", binding.Path, "--noheadings", "--output", "FSTYPE,SOURCE")
			if fields := strings.Fields(string(out)); err != nil || len(fields) != 2 || fields[0] != "cifs" || fields[1] != binding.Source {
				return errors.New("the share is not mounted; check it on the Storage page")
			}
		}
		if _, err := s.run(ctx, "pvesm", "add", "dir", storage, "--path", binding.Path, "--content", "backup", "--is_mountpoint", "yes"); err != nil {
			return errors.New("Proxmox refused the storage")
		}
		if s.Simulation {
			return s.simulateStorage(storage, binding.Path, true)
		}
		return nil
	}

	switch {
	case current == "":
		return nil
	case current != storage:
		return fmt.Errorf("the storage %s wasn't made by Iglo-OS; remove it in Proxmox", current)
	}
	if !s.Simulation {
		// A backup job on this storage would fail, so it has to move first.
		out, err := s.run(ctx, "pvesh", "get", "/cluster/backup/"+BackupJobID, "--output-format", "json")
		var job struct {
			Storage string `json:"storage"`
		}
		if err == nil && json.Unmarshal(out, &job) == nil && job.Storage == storage {
			return errors.New("the backup job uses this share; choose another storage on the Backups page first")
		}
	}
	if _, err := s.run(ctx, "pvesm", "remove", storage); err != nil {
		return errors.New("Proxmox refused to remove the storage")
	}
	if s.Simulation {
		return s.simulateStorage(storage, binding.Path, false)
	}
	return nil
}

// simulateStorage keeps a storage.cfg for the dev fixture, which has no Proxmox.
func (s *Bindings) simulateStorage(storage, path string, on bool) error {
	data, _ := os.ReadFile(s.storageConfig())
	blocks := []string{}
	for _, block := range strings.Split(strings.TrimSpace(string(data)), "\n\n") {
		if block != "" && !strings.HasPrefix(block, "dir: "+storage+"\n") {
			blocks = append(blocks, block)
		}
	}
	if on {
		blocks = append(blocks, fmt.Sprintf("dir: %s\n\tpath %s\n\tcontent backup\n\tis_mountpoint yes", storage, path))
	}
	return os.WriteFile(s.storageConfig(), []byte(strings.Join(blocks, "\n\n")+"\n"), 0o600)
}

func (s *Bindings) registerProxmoxBackups(mux *http.ServeMux) {
	mux.HandleFunc("PUT /v1/storage/bindings/{id}/proxmox-backups", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Enabled *bool `json:"enabled"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 128)).Decode(&in) != nil || in.Enabled == nil {
			http.Error(w, "give enabled true or false", 400)
			return
		}
		if err := s.UseForProxmoxBackups(r.Context(), r.PathValue("id"), *in.Enabled); err != nil {
			http.Error(w, err.Error(), 409)
			return
		}
		w.WriteHeader(204)
	})
}

// UseBindingForBackups turns the Proxmox backups on a Backups share on or off.
func (c *Client) UseBindingForBackups(ctx context.Context, id string, on bool) error {
	return c.do(ctx, http.MethodPut, "/v1/storage/bindings/"+id+"/proxmox-backups", map[string]bool{"enabled": on}, nil)
}
