package setup

import (
	"context"
	"slices"

	"iglo/internal/agent"
	"iglo/internal/apps"
)

// AppChoice is an app on the first page of the setup.
type AppChoice struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Installed   bool   `json:"installed"`
	// Shares are the roles of the NAS shares that the app needs.
	Shares []string `json:"shares"`
	// MemoryMiB is the memory limit of the app, for the review.
	MemoryMiB int `json:"memoryMiB"`
	// DiskGiB is the local disk the app takes, for the review.
	DiskGiB int `json:"diskGiB"`
	// Ready is false when the host isn't prepared yet, with what is Missing.
	Ready   bool     `json:"ready"`
	Missing []string `json:"missing"`
}

// View is the setup for its page: the run, and what the page needs to plan one.
type View struct {
	State string      `json:"state"`
	Steps []Step      `json:"steps"`
	Apps  []AppChoice `json:"apps"`
	// HostBytes is the memory of the host, for the review.
	HostBytes int64 `json:"hostBytes"`
	// Bindings are the shares that are connected, which the setup reuses.
	Bindings []agent.Binding `json:"bindings"`
	Storage  string          `json:"storage"`
	Storages []string        `json:"storages"`
	// StorageSpace is the size of each storage, for the disk budget.
	StorageSpace map[string]apps.StorageSpace `json:"storageSpace"`
}

func (s *Service) View(ctx context.Context) View {
	s.mu.Lock()
	view := View{State: s.state, Steps: []Step{}, Apps: []AppChoice{}, Bindings: []agent.Binding{}, Storages: []string{}}
	for _, step := range s.steps {
		view.Steps = append(view.Steps, *step)
	}
	s.mu.Unlock()

	status, _ := s.Apps.Status(ctx)
	view.Storage, view.Storages, view.StorageSpace = status.Defaults.Storage, status.Defaults.Storages, status.Defaults.StorageSpace
	if view.StorageSpace == nil {
		view.StorageSpace = map[string]apps.StorageSpace{}
	}
	gaming, err := s.Agent.GamingReady(ctx)
	if err != nil {
		gaming = agent.GamingReadiness{Missing: []string{"an answer from the host agent"}}
	}
	for _, id := range Apps {
		choice := AppChoice{ID: id, Shares: shareRoles[id], MemoryMiB: memoryMiB[id], DiskGiB: diskGiB(id), Ready: true, Missing: []string{}}
		if index := slices.IndexFunc(status.Apps, func(app apps.AppView) bool { return app.ID == id }); index >= 0 {
			app := status.Apps[index]
			choice.Name, choice.Description, choice.Installed = app.Name, app.Description, app.Installed
		}
		if id == agent.GamingApp && !gaming.Ready {
			choice.Ready, choice.Missing = false, gaming.Missing
		}
		view.Apps = append(view.Apps, choice)
	}
	if memory, err := s.Agent.HostMemory(ctx); err == nil {
		view.HostBytes = memory.TotalBytes
	}
	if bindings, err := s.Agent.StorageBindings(ctx); err == nil {
		view.Bindings = bindings.Bindings
	}
	return view
}
