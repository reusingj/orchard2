package api

import "testing"

func TestReversedDomain(t *testing.T) {
	got, err := reversedDomain("https://mdm.feuerwehr-wasserlos.de:8443/path")
	if err != nil {
		t.Fatal(err)
	}
	if want := "de.feuerwehr-wasserlos.mdm"; got != want {
		t.Fatalf("reversedDomain() = %q, want %q", got, want)
	}
}

func TestReversedDomainRequiresHost(t *testing.T) {
	if _, err := reversedDomain(""); err == nil {
		t.Fatal("reversedDomain() error = nil, want an error for an empty URL")
	}
}
