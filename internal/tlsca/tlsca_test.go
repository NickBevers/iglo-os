package tlsca

import (
	"crypto/x509"
	"testing"
)

func TestLeafVerifiesAgainstCAAndOnlyForHostname(t *testing.T) {
	authority, err := Load(t.TempDir(), "iglo.local")
	if err != nil {
		t.Fatal(err)
	}

	leaf, err := authority.GetCertificate(nil)
	if err != nil {
		t.Fatal(err)
	}

	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(authority.CertPEM())

	if _, err := leaf.Leaf.Verify(x509.VerifyOptions{Roots: roots, DNSName: "iglo.local"}); err != nil {
		t.Fatalf("leaf does not verify: %v", err)
	}
	if _, err := leaf.Leaf.Verify(x509.VerifyOptions{Roots: roots, DNSName: "example.com"}); err == nil {
		t.Fatal("leaf verified for another hostname")
	}
}

func TestLoadReusesExistingCA(t *testing.T) {
	dir := t.TempDir()

	first, err := Load(dir, "iglo.local")
	if err != nil {
		t.Fatal(err)
	}
	second, err := Load(dir, "iglo.local")
	if err != nil {
		t.Fatal(err)
	}

	if string(first.CertPEM()) != string(second.CertPEM()) {
		t.Fatal("CA was regenerated")
	}
}

func TestLeafCoversAppNamesWithTheSameCA(t *testing.T) {
	dir := t.TempDir()
	// A CA from an older install, made without app names.
	if _, err := Load(dir, "iglo.local"); err != nil {
		t.Fatal(err)
	}

	authority, err := Load(dir, "iglo.local", "seerr.iglo.local", "jellyfin.iglo.local")
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := authority.GetCertificate(nil)
	if err != nil {
		t.Fatal(err)
	}

	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(authority.CertPEM())
	for _, name := range []string{"iglo.local", "seerr.iglo.local", "jellyfin.iglo.local"} {
		if _, err := leaf.Leaf.Verify(x509.VerifyOptions{Roots: roots, DNSName: name}); err != nil {
			t.Errorf("%s does not verify: %v", name, err)
		}
	}

	// The name limit of the CA still holds for names outside iglo.local.
	outside, err := Load(dir, "iglo.local", "seerr.example.com")
	if err != nil {
		t.Fatal(err)
	}
	cert, err := outside.GetCertificate(nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cert.Leaf.Verify(x509.VerifyOptions{Roots: roots, DNSName: "seerr.example.com"}); err == nil {
		t.Fatal("the CA signed a name outside iglo.local")
	}
}
