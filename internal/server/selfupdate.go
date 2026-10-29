package server

import "net/http"

// selfUpdateStatus shows the running version and the last upgrade. Upgrades
// come from "make deploy" on the owner's machine, not from this page.
func (s *server) selfUpdateStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.SelfUpdate.Status(r.Context()))
}
