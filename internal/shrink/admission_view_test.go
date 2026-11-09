package shrink

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestShrinkStatusReportsAdmission(t *testing.T) {
	s, p := fixture(t)
	job := submit(t, s, p)
	lease := filepath.Join(s.AdmissionDir, "admit-until")
	for _, test := range []struct {
		name, value string
		admitted    bool
	}{
		{"native", "-1", true},
		{"held", "0", false},
		{"leased", strconv.FormatInt(time.Now().Add(time.Minute).Unix(), 10), true},
		{"expired", strconv.FormatInt(time.Now().Add(-time.Minute).Unix(), 10), false},
		{"unreadable lease", "invalid", false},
		{"missing lease", "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.value == "" {
				if err := os.Remove(lease); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(lease, []byte(test.value), 0600); err != nil {
				t.Fatal(err)
			}
			for _, paused := range []bool{true, false} {
				if err := s.Settings(paused); err != nil {
					t.Fatal(err)
				}
				view := s.Status()
				if view.Admitted != test.admitted || view.Paused != paused || len(view.Jobs) != 1 || view.Jobs[0].ID != job.ID || view.Jobs[0].State != "queued" {
					t.Fatalf("view: %+v", view)
				}
				w := httptest.NewRecorder()
				s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/shrink", nil))
				var body struct {
					Admitted *bool `json:"admitted"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Admitted == nil || *body.Admitted != test.admitted {
					t.Fatalf("HTTP view: %s, %v", w.Body, err)
				}
			}
		})
	}
}
