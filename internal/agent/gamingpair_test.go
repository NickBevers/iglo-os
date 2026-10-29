package agent

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestWolfPairingsHideTheSecret(t *testing.T) {
	data := []byte(`{"success":true,"requests":[{"pair_secret":"s3cret-one","client_ip":"192.168.1.30"},{"pair_secret":"","client_ip":"192.168.1.31"}]}`)
	pairings, secrets, err := wolfPairings(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(pairings) != 1 || pairings[0].ClientIP != "192.168.1.30" || !gamingPairingID.MatchString(pairings[0].ID) {
		t.Fatalf("pairings = %+v", pairings)
	}
	if secrets[pairings[0].ID] != "s3cret-one" {
		t.Fatalf("secrets = %v", secrets)
	}
	// What the manager gets never holds the secret.
	shown, _ := json.Marshal(pairings)
	if strings.Contains(string(shown), "s3cret") {
		t.Fatalf("secret in %s", shown)
	}

	if _, _, err := wolfPairings([]byte("not json")); err == nil {
		t.Fatal("accepted an invalid list")
	}
}

func TestPairGamingDeviceChecksItsAnswers(t *testing.T) {
	id := pairingID("s3cret-one")
	for _, answers := range [][2]string{{id, "123"}, {id, "12345"}, {id, "12a4"}, {id, "1234\n"}, {"../x", "1234"}, {"", "1234"}} {
		if err := pairGamingDevice(t.Context(), 120, answers[0], answers[1]); !errors.Is(err, ErrInvalidAnswers) {
			t.Errorf("%q: err = %v", answers, err)
		}
	}
}
