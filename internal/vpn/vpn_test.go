package vpn

import "testing"

func TestKnownLocations(t *testing.T) {
	if Known.Gluetun == "" || len(Known.ProtonVPN) < 50 || len(Known.PIA) < 50 {
		t.Fatalf("locations = %s, %d, %d", Known.Gluetun, len(Known.ProtonVPN), len(Known.PIA))
	}
}

func TestValidLocations(t *testing.T) {
	for name, test := range map[string]struct {
		provider  string
		locations []string
		valid     bool
	}{
		"proton country":       {ProtonVPN, []string{"Netherlands", "Switzerland"}, true},
		"pia region":           {PIA, []string{"DE Berlin", "Netherlands"}, true},
		"pia dedicated":        {PIA, []string{Dedicated}, true},
		"dedicated and more":   {PIA, []string{Dedicated, "Netherlands"}, false},
		"proton dedicated":     {ProtonVPN, []string{Dedicated}, false},
		"pia region on proton": {ProtonVPN, []string{"DE Berlin"}, false},
		"unknown":              {ProtonVPN, []string{"Atlantis"}, false},
		"none":                 {ProtonVPN, []string{}, false},
		"empty":                {ProtonVPN, []string{""}, false},
		"twice":                {ProtonVPN, []string{"Netherlands", "Netherlands"}, false},
		"eleven":               {ProtonVPN, Known.ProtonVPN[:11], false},
	} {
		if got := ValidLocations(test.provider, test.locations); got != test.valid {
			t.Errorf("%s: valid = %v", name, got)
		}
	}
}
