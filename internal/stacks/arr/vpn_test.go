package arr

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testWireGuardKey = "cGVyZmVjdGx5IHZhbGlkIGtleSBvZiAzMiBieXRlcyE="

// oldEnv is the .env of a stack from before the provider choice.
const oldEnv = "TZ=Europe/Brussels\nWIREGUARD_PRIVATE_KEY=" + testWireGuardKey + "\nVPN_COUNTRIES=Netherlands\nARR_USERNAME=iglo\n"

func writeEnv(t *testing.T, content string) (envPath, gluetunDir string) {
	t.Helper()
	dir := t.TempDir()
	envPath, gluetunDir = filepath.Join(dir, ".env"), filepath.Join(dir, "gluetun")
	os.MkdirAll(gluetunDir, 0o700)
	os.WriteFile(envPath, []byte(content), 0o600)
	return envPath, gluetunDir
}

func readTestEnv(t *testing.T, path string) map[string]string {
	t.Helper()
	data, _ := os.ReadFile(path)
	return parseEnv(string(data))
}

// fakePIA answers the token and dedicated IP calls of PIA's API.
func fakePIA(t *testing.T, dedicated string) *PIAClient {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			if r.FormValue("username") != "p1234567" || r.FormValue("password") != "pa$$ word" {
				json.NewEncoder(w).Encode(map[string]string{"token": ""})
				return
			}
			json.NewEncoder(w).Encode(map[string]string{"token": "session"})
		case "/dedicated_ip":
			var body struct {
				Tokens []string `json:"tokens"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			if r.Header.Get("Authorization") != "Token session" || len(body.Tokens) != 1 {
				http.Error(w, "no", http.StatusUnauthorized)
				return
			}
			w.Write([]byte(dedicated))
		}
	}))
	t.Cleanup(server.Close)
	return &PIAClient{BaseURL: server.URL, HTTP: server.Client()}
}

// envHasNoSecrets fails when a secret is in .env, which the backups of the
// container copy.
func envHasNoSecrets(t *testing.T, path string, secrets ...string) {
	t.Helper()
	data, _ := os.ReadFile(path)
	for _, secret := range secrets {
		if secret != "" && strings.Contains(string(data), secret) {
			t.Fatalf(".env has a secret:\n%s", data)
		}
	}
	for _, key := range secretKeys {
		if strings.Contains(string(data), key+"=") {
			t.Fatalf(".env has %s:\n%s", key, data)
		}
	}
}

func TestApplyVPNMovesTheOldKeyOutOfEnv(t *testing.T) {
	envPath, gluetunDir := writeEnv(t, oldEnv)
	choice := VPN{Provider: "protonvpn", Locations: []string{"Switzerland", " Netherlands"}}
	secrets, err := ApplyVPN(context.Background(), choice, envPath, gluetunDir, nil)
	if err != nil {
		t.Fatal(err)
	}

	// The key of a stack from before the sealed secrets comes out of .env, for the host.
	if secrets["wireguard_private_key"] != testWireGuardKey || len(secrets) != len(SecretFiles) || secrets["openvpn_user"] != "" {
		t.Fatalf("secrets = %v", secrets)
	}
	env := readTestEnv(t, envPath)
	if env["VPN_COUNTRIES"] != "Switzerland,Netherlands" || env["GLUETUN_PROVIDER"] != "protonvpn" ||
		env["ARR_USERNAME"] != "iglo" || env["TZ"] != "Europe/Brussels" {
		t.Fatalf("env = %v", env)
	}
	envHasNoSecrets(t, envPath, testWireGuardKey)
	if info, _ := os.Stat(envPath); info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", info.Mode())
	}
}

func TestApplyVPNQuotesAnApostrophe(t *testing.T) {
	envPath, gluetunDir := writeEnv(t, oldEnv)
	choice := VPN{Provider: "protonvpn", Locations: []string{"Cote d'Ivoire", "Netherlands"}, WireGuardKey: testWireGuardKey}
	if _, err := ApplyVPN(context.Background(), choice, envPath, gluetunDir, nil); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(envPath)
	// Compose v5.1.2 reads \' inside single quotes as a quote.
	if !strings.Contains(string(data), `VPN_COUNTRIES='Cote d\'Ivoire,Netherlands'`) {
		t.Fatalf(".env = %s", data)
	}
	if env := readTestEnv(t, envPath); env["VPN_COUNTRIES"] != "Cote d'Ivoire,Netherlands" {
		t.Fatalf("read back = %q", env["VPN_COUNTRIES"])
	}
}

func TestApplyVPNSwitchesToPIA(t *testing.T) {
	envPath, gluetunDir := writeEnv(t, oldEnv)

	// PIA without its login is refused, and .env stays as it was.
	if _, err := ApplyVPN(context.Background(), VPN{Provider: "pia", Locations: []string{"Netherlands"}}, envPath, gluetunDir, nil); err == nil {
		t.Fatal("PIA without a login passed")
	}
	if data, _ := os.ReadFile(envPath); string(data) != oldEnv {
		t.Fatalf("env changed: %s", data)
	}

	// Passwords may have any character but a line break: they never go in .env.
	password := `pa$$ 'word\`
	choice := VPN{Provider: "pia", Locations: []string{"DE Berlin", "Netherlands"}, PIAUsername: "p1234567", PIAPassword: password}
	secrets, err := ApplyVPN(context.Background(), choice, envPath, gluetunDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	env := readTestEnv(t, envPath)
	if env["GLUETUN_PROVIDER"] != "private internet access" || env["VPN_REGIONS"] != "DE Berlin,Netherlands" ||
		env["VPN_COUNTRIES"] != "" || env["GLUETUN_PORT_FORWARD_ONLY"] != "on" {
		t.Fatalf("env = %v", env)
	}
	if secrets["openvpn_user"] != "p1234567" || secrets["openvpn_password"] != password ||
		secrets["vpn_port_forwarding_username"] != "p1234567" || secrets["vpn_port_forwarding_password"] != password ||
		secrets["wireguard_private_key"] != "" {
		t.Fatalf("secrets = %v", secrets)
	}
	envHasNoSecrets(t, envPath, password, "p1234567", testWireGuardKey)

	if _, err := ApplyVPN(context.Background(), VPN{Provider: "pia", Locations: []string{"Netherlands"}, PIAUsername: "p1", PIAPassword: "a\nb"}, envPath, gluetunDir, nil); err == nil {
		t.Fatal("a password with a line break passed")
	}
}

func TestApplyVPNDedicatedIP(t *testing.T) {
	envPath, gluetunDir := writeEnv(t, oldEnv)
	token := "DIP0123456789abcdefghijklmnopqrs"
	pia := fakePIA(t, `[{"status":"active","ip":"212.102.35.10","cn":"amsterdam401","id":"nl_amsterdam"}]`)

	choice := VPN{Provider: "pia", Locations: []string{"Dedicated IP"}, PIAUsername: "p1234567", PIAPassword: "pa$$ word", PIADedicatedToken: token}
	secrets, err := ApplyVPN(context.Background(), choice, envPath, gluetunDir, pia)
	if err != nil {
		t.Fatal(err)
	}
	env := readTestEnv(t, envPath)
	if env["GLUETUN_PROVIDER"] != "custom" || env["GLUETUN_SERVER_NAMES"] != "amsterdam401" || env["GLUETUN_PORT_FORWARD_ONLY"] != "off" ||
		env["GLUETUN_PORT_FORWARDING_PROVIDER"] != "private internet access" || env["GLUETUN_OPENVPN_CONFIG"] != "/gluetun/pia-dedicated.ovpn" ||
		env["VPN_REGIONS"] != "" {
		t.Fatalf("env = %v", env)
	}
	if secrets["openvpn_user"] != "dedicated_ip_"+token || secrets["openvpn_password"] != "" ||
		secrets["vpn_port_forwarding_username"] != "p1234567" || secrets["vpn_port_forwarding_password"] != "pa$$ word" {
		t.Fatalf("secrets = %v", secrets)
	}
	envHasNoSecrets(t, envPath, token, "pa$$ word")
	config, _ := os.ReadFile(filepath.Join(gluetunDir, "pia-dedicated.ovpn"))
	if !strings.Contains(string(config), "remote 212.102.35.10 8080\n") || !strings.Contains(string(config), "-----BEGIN CERTIFICATE-----") ||
		strings.Contains(string(config), token) {
		t.Fatalf("config = %s", config)
	}
}

func TestApplyVPNRefusesBadDedicatedIPs(t *testing.T) {
	token := "DIP0123456789abcdefghijklmnopqrs"
	for name, answer := range map[string]string{
		"expired":   `[{"status":"expired","ip":"1.2.3.4","cn":"x","id":"nl"}]`,
		"in the US": `[{"status":"active","ip":"1.2.3.4","cn":"newjersey","id":"us_newjersey"}]`,
		"odd host":  `[{"status":"active","ip":"1.2.3.4\nremote evil","cn":"x","id":"nl"}]`,
	} {
		envPath, gluetunDir := writeEnv(t, oldEnv)
		choice := VPN{Provider: "pia", Locations: []string{"Dedicated IP"}, PIAUsername: "p1234567", PIAPassword: "pa$$ word", PIADedicatedToken: token}
		if _, err := ApplyVPN(context.Background(), choice, envPath, gluetunDir, fakePIA(t, answer)); err == nil {
			t.Errorf("%s: passed", name)
		}
	}

	// A wrong password never reaches the dedicated IP call.
	envPath, gluetunDir := writeEnv(t, oldEnv)
	choice := VPN{Provider: "pia", Locations: []string{"Dedicated IP"}, PIAUsername: "p1234567", PIAPassword: "wrong", PIADedicatedToken: token}
	if _, err := ApplyVPN(context.Background(), choice, envPath, gluetunDir, fakePIA(t, "[]")); err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("wrong password: %v", err)
	}
}
