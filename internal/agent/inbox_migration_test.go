package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInboxMigrationRestartsLegacyOnPrecommitFailures(t *testing.T) {
	data, err := os.ReadFile("../../deploy/stacks/inbox/install.sh")
	if err != nil {
		t.Fatal(err)
	}
	_, functions, _ := strings.Cut(string(data), "native_ready() {")
	functions = "native_ready() {" + functions
	functions = strings.TrimSuffix(functions, "scoped_main\n")
	for _, failure := range []string{"copy", "start", "tags", "none"} {
		t.Run(failure, func(t *testing.T) {
			log := filepath.Join(t.TempDir(), "calls")
			script := "set -euo pipefail\nCT_ID=180\n" + functions + `
log(){ :; }
die(){ exit 1; }
native_ready(){ return 1; }
load_scoped_answers(){ :; }
check_scoped_answers(){ :; }
app_description(){ echo inbox; }
scoped_files(){ [[ "$failure" != copy ]]; }
install_native(){ [[ "$failure" != start ]]; }
pct(){
 printf '%s\n' "$*" >> "$call_log"
 if [[ "$1" == pull ]]; then touch "$4"; fi
 if [[ "$1" == set && "$failure" == tags ]]; then return 1; fi
}
failure=$1
call_log=$2
migrate_stack
`
			output, err := exec.Command("bash", "-c", script, "migration", failure, log).CombinedOutput()
			calls, _ := os.ReadFile(log)
			if failure == "none" {
				if err != nil {
					t.Fatalf("%v %s", err, output)
				}
				if strings.Contains(string(calls), "disable --now iglo-inbox") {
					t.Fatal("rolled back successful migration")
				}
			} else {
				if err == nil {
					t.Fatal("migration reported success")
				}
				if !strings.Contains(string(calls), "disable --now iglo-inbox") || !strings.Contains(string(calls), "docker compose start") {
					t.Fatalf("FileFlows not restored: %s %s", calls, output)
				}
			}
		})
	}
}
