package backups

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"iglo/internal/proxmox"
)

const inventoryTTL = time.Minute

type backupInventory struct {
	storages []StorageView
	backups  map[int][]BackupView
}

type inventoryCache struct {
	mu         sync.Mutex
	group      singleflight.Group
	generation uint64
	key        string
	at         time.Time
	value      *backupInventory
}

func (s *Service) forgetInventory() {
	s.inventory.mu.Lock()
	s.inventory.generation++
	s.inventory.value = nil
	s.inventory.mu.Unlock()
}

// Inventory only caches storage and archive lists. Resources still come from
// the shared Proxmox client, and destructive actions look up archives afresh.
func (s *Service) backupInventory(ctx context.Context, resources []proxmox.Resource) (*backupInventory, error) {
	var nodes []string
	for _, resource := range resources {
		if resource.Type == "node" && resource.Status == "online" {
			nodes = append(nodes, resource.Node)
		}
	}
	slices.Sort(nodes)
	key := strings.Join(nodes, ",")
	cache := &s.inventory
	cache.mu.Lock()
	if cache.value != nil && key == cache.key && time.Since(cache.at) < inventoryTTL {
		value := cache.value
		cache.mu.Unlock()
		return value, nil
	}
	generation := cache.generation
	cache.mu.Unlock()
	result, err, _ := cache.group.Do(fmt.Sprintf("%d:%s", generation, key), func() (any, error) {
		// Another caller can finish between the first cache check and Do.
		cache.mu.Lock()
		if cache.value != nil && key == cache.key && generation == cache.generation && time.Since(cache.at) < inventoryTTL {
			value := cache.value
			cache.mu.Unlock()
			return value, nil
		}
		cache.mu.Unlock()
		value, complete, err := s.loadInventory(ctx, nodes)
		if err == nil && complete {
			cache.mu.Lock()
			if generation == cache.generation {
				cache.value, cache.key, cache.at = value, key, time.Now()
			}
			cache.mu.Unlock()
		}
		return value, err
	})
	if err != nil {
		return nil, err
	}
	return result.(*backupInventory), nil
}

func (s *Service) loadInventory(ctx context.Context, nodes []string) (*backupInventory, bool, error) {
	storages := make([][]proxmox.BackupStorage, len(nodes))
	errors := make([]error, len(nodes))
	var group sync.WaitGroup
	for index, node := range nodes {
		group.Go(func() { storages[index], errors[index] = s.Proxmox.BackupStorages(ctx, node) })
	}
	group.Wait()
	inventory := &backupInventory{storages: []StorageView{}, backups: map[int][]BackupView{}}
	type location struct{ node, storage string }
	var locations []location
	shared := map[string]bool{}
	for index, node := range nodes {
		if errors[index] != nil {
			return nil, false, errors[index]
		}
		for _, storage := range storages[index] {
			if bool(storage.Shared) && shared[storage.Storage] {
				continue
			}
			if storage.Shared {
				shared[storage.Storage] = true
			}
			inventory.storages = append(inventory.storages, StorageView{Node: node, Name: storage.Storage, Type: storage.Type, Total: storage.Total, Used: storage.Used})
			locations = append(locations, location{node, storage.Storage})
		}
	}
	archives := make([][]proxmox.Backup, len(locations))
	errors = make([]error, len(locations))
	for index, location := range locations {
		group.Go(func() { archives[index], errors[index] = s.Proxmox.Backups(ctx, location.node, location.storage) })
	}
	group.Wait()
	complete := true
	for index, location := range locations {
		if errors[index] != nil {
			complete = false
			s.Logger.Warn("could not list backups", "storage", location.storage, "error", errors[index])
			continue
		}
		for _, backup := range archives[index] {
			inventory.backups[backup.VMID] = append(inventory.backups[backup.VMID], BackupView{
				VolID: backup.VolID, Storage: location.storage, CreatedAt: time.Unix(backup.CTime, 0),
				Size: backup.Size, Notes: backup.Notes, Protected: bool(backup.Protected),
			})
		}
	}
	return inventory, complete, nil
}
