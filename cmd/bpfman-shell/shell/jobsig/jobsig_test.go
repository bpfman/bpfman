package jobsig

import (
	"syscall"
	"testing"
)

func TestFromNameAndShortNameRoundTrip(t *testing.T) {
	t.Parallel()
	for _, e := range table {
		sig, ok := FromName(e.name)
		if !ok || sig != e.sig {
			t.Errorf("FromName(%q) = (%v, %v), want (%v, true)", e.name, sig, ok, e.sig)
		}

		if got := ShortName(e.sig); got != e.name {
			t.Errorf("ShortName(%v) = %q, want %q", e.sig, got, e.name)
		}
	}
}

func TestFromNameNormalisation(t *testing.T) {
	t.Parallel()
	cases := []string{"USR1", "usr1", "SIGUSR1", "sigusr1", " USR1 ", "\tSIGUSR1\n"}
	for _, in := range cases {
		sig, ok := FromName(in)
		if !ok || sig != syscall.SIGUSR1 {
			t.Errorf("FromName(%q) = (%v, %v), want (SIGUSR1, true)", in, sig, ok)
		}

		if !KnownName(in) {
			t.Errorf("KnownName(%q) = false, want true", in)
		}
	}
}

func TestUnknownName(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"", "BOGUS", "SIGBOGUS", "9", "SIG"} {
		if sig, ok := FromName(in); ok {
			t.Errorf("FromName(%q) = (%v, true), want not ok", in, sig)
		}

		if KnownName(in) {
			t.Errorf("KnownName(%q) = true, want false", in)
		}
	}
}

func TestShortNameNumericFallback(t *testing.T) {
	t.Parallel()
	if got := ShortName(syscall.Signal(99)); got != "99" {
		t.Errorf("ShortName(99) = %q, want \"99\"", got)
	}
}
