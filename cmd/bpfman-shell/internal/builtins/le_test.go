package builtins

import (
	"testing"

	"github.com/bpfman/bpfman/cmd/bpfman-shell/driver"
	"github.com/bpfman/bpfman/cmd/bpfman-shell/shell/runtime"
)

// litArg fabricates a literal-text Arg so the helpers can be
// driven without going through the parser.
func litArg(s string) runtime.Arg {
	return runtime.ScalarValueArg{Text: s}
}

// leCtx wraps args in a minimal driver.Ctx for the LE-hex
// handlers. Only Args is consulted; Ctx is set for symmetry with
// other handlers.
func leCtx(t *testing.T, args []runtime.Arg) driver.Ctx {
	return driver.Ctx{Ctx: t.Context(), Args: args}
}

func TestU32LE_FormatsLittleEndian(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want string
	}{
		{"0", "00000000"},
		{"1", "01000000"},
		{"255", "ff000000"},
		{"256", "00010000"},
		{"12345", "39300000"},
		{"4294967295", "ffffffff"},
		{"0x3039", "39300000"},
		{"0xdeadbeef", "efbeadde"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			t.Parallel()
			v, err := HandleU32LE(leCtx(t, []runtime.Arg{litArg(c.in)}))
			if err != nil {
				t.Fatalf("u32le %s: %v", c.in, err)
			}

			got, _ := v.Scalar()
			if got != c.want {
				t.Errorf("u32le %s: got %q want %q", c.in, got, c.want)
			}
		})
	}
}

func TestU64LE_FormatsLittleEndian(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want string
	}{
		{"0", "0000000000000000"},
		{"1", "0100000000000000"},
		{"42", "2a00000000000000"},
		{"18446744073709551615", "ffffffffffffffff"},
		{"0xdeadbeefcafebabe", "bebafecaefbeadde"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			t.Parallel()
			v, err := HandleU64LE(leCtx(t, []runtime.Arg{litArg(c.in)}))
			if err != nil {
				t.Fatalf("u64le %s: %v", c.in, err)
			}

			got, _ := v.Scalar()
			if got != c.want {
				t.Errorf("u64le %s: got %q want %q", c.in, got, c.want)
			}
		})
	}
}

func TestU32LE_Rejects(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		args []runtime.Arg
	}{
		{"no args", nil},
		{"two args", []runtime.Arg{litArg("1"), litArg("2")}},
		{"empty", []runtime.Arg{litArg("")}},
		{"negative", []runtime.Arg{litArg("-1")}},
		{"non-integer", []runtime.Arg{litArg("abc")}},
		{"overflow u32", []runtime.Arg{litArg("4294967296")}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if _, err := HandleU32LE(leCtx(t, c.args)); err == nil {
				t.Fatalf("u32le %v: expected error, got nil", c.args)
			}
		})
	}
}

func TestU64LE_Rejects(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		args []runtime.Arg
	}{
		{"no args", nil},
		{"negative", []runtime.Arg{litArg("-1")}},
		{"non-integer", []runtime.Arg{litArg("xyz")}},
		{"overflow u64", []runtime.Arg{litArg("18446744073709551616")}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if _, err := HandleU64LE(leCtx(t, c.args)); err == nil {
				t.Fatalf("u64le %v: expected error, got nil", c.args)
			}
		})
	}
}
