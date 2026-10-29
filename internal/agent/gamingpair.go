package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
)

// Pairing a Moonlight device: the device shows a PIN and waits, Wolf lists it
// with a pair secret, and Wolf pairs it when it gets that secret and the PIN.
// Checked against Wolf facb8e0 (the pinned stable image, built 29 September
// 2026): GET /api/v1/pair/pending and POST /api/v1/pair/client.
//
// The pair secret is a credential, so it never leaves the agent. The manager
// gets an ID instead, a hash of the secret, and the agent looks the secret up
// again to pair.

// GamingPairing is a Moonlight device that waits to pair.
type GamingPairing struct {
	ID       string `json:"id"`
	ClientIP string `json:"clientIp"`
}

var (
	// Moonlight shows a PIN of four digits.
	moonlightPIN       = regexp.MustCompile(`^[0-9]{4}$`)
	gamingPairingID    = regexp.MustCompile(`^[0-9a-f]{16}$`)
	ErrNoGamingPairing = errors.New("this device no longer waits to pair; start the pairing in Moonlight again")
)

// CheckPairingAnswers checks the ID and PIN of a pairing, in the manager and
// again in the agent.
func CheckPairingAnswers(id, pin string) error {
	if !gamingPairingID.MatchString(id) || !moonlightPIN.MatchString(pin) {
		return fmt.Errorf("%w: give the PIN of four digits that Moonlight shows", ErrInvalidAnswers)
	}
	return nil
}

func pairingID(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:8])
}

// wolfPairings reads the pending list of Wolf: what the manager may see, and
// the secret of each ID.
func wolfPairings(data []byte) ([]GamingPairing, map[string]string, error) {
	var answer struct {
		Requests []struct {
			Secret   string `json:"pair_secret"`
			ClientIP string `json:"client_ip"`
		} `json:"requests"`
	}
	if err := json.Unmarshal(data, &answer); err != nil {
		return nil, nil, errors.New("Wolf sent an invalid pairing list")
	}
	pairings, secrets := []GamingPairing{}, map[string]string{}
	for _, request := range answer.Requests {
		if request.Secret == "" {
			continue
		}
		id := pairingID(request.Secret)
		pairings = append(pairings, GamingPairing{ID: id, ClientIP: request.ClientIP})
		secrets[id] = request.Secret
	}
	return pairings, secrets, nil
}

func pendingGamingPairings(ctx context.Context, vmid int) ([]GamingPairing, map[string]string, error) {
	result, err := wolfCall(ctx, vmid, http.MethodGet, "/api/v1/pair/pending", nil)
	if err != nil {
		return nil, nil, err
	}
	return wolfPairings(result)
}

// pairGamingDevice gives Wolf the PIN of a waiting device. wolfCall sends the
// secret and the PIN on stdin, not as an argument.
func pairGamingDevice(ctx context.Context, vmid int, id, pin string) error {
	if err := CheckPairingAnswers(id, pin); err != nil {
		return err
	}
	_, secrets, err := pendingGamingPairings(ctx, vmid)
	if err != nil {
		return err
	}
	secret, ok := secrets[id]
	if !ok {
		return ErrNoGamingPairing
	}
	body, _ := json.Marshal(map[string]string{"pair_secret": secret, "pin": pin})
	_, err = wolfCall(ctx, vmid, http.MethodPost, "/api/v1/pair/client", body)
	return err
}

func registerGamingPairing(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/gaming/{vmid}/pairing", func(w http.ResponseWriter, r *http.Request) {
		vmid, _ := strconv.Atoi(r.PathValue("vmid"))
		pairings, _, err := pendingGamingPairings(r.Context(), vmid)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(pairings)
	})
	mux.HandleFunc("POST /v1/gaming/{vmid}/pairing/{id}", func(w http.ResponseWriter, r *http.Request) {
		vmid, _ := strconv.Atoi(r.PathValue("vmid"))
		var input struct {
			PIN string `json:"pin"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 128)).Decode(&input) != nil {
			http.Error(w, ErrInvalidAnswers.Error(), http.StatusBadRequest)
			return
		}
		switch err := pairGamingDevice(r.Context(), vmid, r.PathValue("id"), input.PIN); {
		case errors.Is(err, ErrInvalidAnswers):
			http.Error(w, "give the PIN of four digits that Moonlight shows", http.StatusBadRequest)
		case errors.Is(err, ErrNoGamingPairing):
			http.Error(w, err.Error(), http.StatusConflict)
		case err != nil:
			http.Error(w, err.Error(), http.StatusBadGateway)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	})
}

// GamingPairings lists the Moonlight devices that wait to pair.
func (c *Client) GamingPairings(ctx context.Context, vmid int) ([]GamingPairing, error) {
	var pairings []GamingPairing
	err := c.do(ctx, http.MethodGet, "/v1/gaming/"+strconv.Itoa(vmid)+"/pairing", nil, &pairings)
	return pairings, err
}

// PairGamingDevice pairs a waiting Moonlight device with the PIN it shows.
func (c *Client) PairGamingDevice(ctx context.Context, vmid int, id, pin string) error {
	return c.do(ctx, http.MethodPost, "/v1/gaming/"+strconv.Itoa(vmid)+"/pairing/"+url.PathEscape(id), map[string]string{"pin": pin}, nil)
}
