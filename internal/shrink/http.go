package shrink

import (
	"encoding/json"
	"net/http"
)

func (s *Service) Subscribe() (chan string, func()) {
	c := make(chan string, 32)
	s.mu.Lock()
	s.subs[c] = struct{}{}
	s.mu.Unlock()
	return c, func() { s.mu.Lock(); delete(s.subs, c); s.mu.Unlock() }
}
func (s *Service) Register(mux *http.ServeMux) {
	write := func(w http.ResponseWriter, code int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		json.NewEncoder(w).Encode(v)
	}
	mux.HandleFunc("GET /library", func(w http.ResponseWriter, r *http.Request) { write(w, 200, s.Library()) })
	mux.HandleFunc("POST /library/scan", func(w http.ResponseWriter, r *http.Request) { s.RequestScan(); w.WriteHeader(202) })
	mux.HandleFunc("GET /shrink", func(w http.ResponseWriter, r *http.Request) { write(w, 200, s.Status()) })
	mux.HandleFunc("POST /shrink", func(w http.ResponseWriter, r *http.Request) {
		var in Input
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 512<<10)).Decode(&in) != nil {
			http.Error(w, "invalid shrink request", 400)
			return
		}
		jobs, err := s.Submit(in)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		write(w, 202, jobs)
	})
	mux.HandleFunc("PUT /shrink/settings", func(w http.ResponseWriter, r *http.Request) {
		var in Settings
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 512)).Decode(&in) != nil || in.Paused == nil {
			http.Error(w, "give paused", 400)
			return
		}
		if err := s.Settings(*in.Paused); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.WriteHeader(204)
	})
	for _, action := range []string{"cancel", "rescanned"} {
		mux.HandleFunc("POST /shrink/"+action+"/{id}", func(w http.ResponseWriter, r *http.Request) {
			id := r.PathValue("id")
			if !validID(id) {
				http.Error(w, "invalid job", 400)
				return
			}
			var err error
			if action == "cancel" {
				err = s.Cancel(id)
			} else {
				err = s.Rescanned(id)
			}
			if err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			w.WriteHeader(204)
		})
	}
}
func (s *Service) Handler() http.Handler { mux := http.NewServeMux(); s.Register(mux); return mux }
