package dispatcher_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/vishvananda/netlink"

	"github.com/bpfman/bpfman/dispatcher"
)

func TestLoadTCDispatcher(t *testing.T) {
	t.Parallel()

	if os.Getuid() != 0 {
		t.Skip("requires root")
	}

	// Create config for 1 program
	cfg, err := dispatcher.NewTCConfig(1)
	if err != nil {
		t.Fatalf("NewTCConfig: %v", err)
	}

	// Load the dispatcher spec
	spec, err := dispatcher.LoadTCDispatcher(cfg)
	if err != nil {
		t.Fatalf("LoadTCDispatcher: %v", err)
	}

	t.Logf("Loaded TC dispatcher spec with %d programs", len(spec.Programs))
	for name, prog := range spec.Programs {
		t.Logf("  Program: %s (type: %s)", name, prog.Type)
	}
}

func TestNewXDPConfig(t *testing.T) {
	t.Parallel()

	t.Run("valid range", func(t *testing.T) {
		t.Parallel()
		for n := 1; n <= dispatcher.MaxPrograms; n++ {
			cfg, err := dispatcher.NewXDPConfig(n)
			if err != nil {
				t.Fatalf("NewXDPConfig(%d): unexpected error: %v", n, err)
			}
			if int(cfg.NumProgsEnabled) != n {
				t.Errorf("NewXDPConfig(%d): NumProgsEnabled = %d", n, cfg.NumProgsEnabled)
			}
		}
	})

	t.Run("default priorities", func(t *testing.T) {
		t.Parallel()
		cfg, err := dispatcher.NewXDPConfig(1)
		if err != nil {
			t.Fatal(err)
		}
		for i := range dispatcher.MaxPrograms {
			if cfg.RunPrios[i] != dispatcher.DispatcherRunPriority {
				t.Errorf("RunPrios[%d] = %d, want %d", i, cfg.RunPrios[i], dispatcher.DispatcherRunPriority)
			}
		}
	})

	t.Run("zero", func(t *testing.T) {
		t.Parallel()
		if _, err := dispatcher.NewXDPConfig(0); err == nil {
			t.Error("NewXDPConfig(0): expected error")
		}
	})

	t.Run("negative", func(t *testing.T) {
		t.Parallel()
		if _, err := dispatcher.NewXDPConfig(-1); err == nil {
			t.Error("NewXDPConfig(-1): expected error")
		}
	})

	t.Run("exceeds max", func(t *testing.T) {
		t.Parallel()
		if _, err := dispatcher.NewXDPConfig(dispatcher.MaxPrograms + 1); err == nil {
			t.Errorf("NewXDPConfig(%d): expected error", dispatcher.MaxPrograms+1)
		}
	})
}

func TestNewTCConfig(t *testing.T) {
	t.Parallel()

	t.Run("valid range", func(t *testing.T) {
		t.Parallel()
		for n := 1; n <= dispatcher.MaxPrograms; n++ {
			cfg, err := dispatcher.NewTCConfig(n)
			if err != nil {
				t.Fatalf("NewTCConfig(%d): unexpected error: %v", n, err)
			}
			if int(cfg.NumProgsEnabled) != n {
				t.Errorf("NewTCConfig(%d): NumProgsEnabled = %d", n, cfg.NumProgsEnabled)
			}
		}
	})

	t.Run("default priorities", func(t *testing.T) {
		t.Parallel()
		cfg, err := dispatcher.NewTCConfig(1)
		if err != nil {
			t.Fatal(err)
		}
		for i := range dispatcher.MaxPrograms {
			if cfg.RunPrios[i] != dispatcher.DispatcherRunPriority {
				t.Errorf("RunPrios[%d] = %d, want %d", i, cfg.RunPrios[i], dispatcher.DispatcherRunPriority)
			}
		}
	})

	t.Run("zero", func(t *testing.T) {
		t.Parallel()
		if _, err := dispatcher.NewTCConfig(0); err == nil {
			t.Error("NewTCConfig(0): expected error")
		}
	})

	t.Run("negative", func(t *testing.T) {
		t.Parallel()
		if _, err := dispatcher.NewTCConfig(-1); err == nil {
			t.Error("NewTCConfig(-1): expected error")
		}
	})

	t.Run("exceeds max", func(t *testing.T) {
		t.Parallel()
		if _, err := dispatcher.NewTCConfig(dispatcher.MaxPrograms + 1); err == nil {
			t.Errorf("NewTCConfig(%d): expected error", dispatcher.MaxPrograms+1)
		}
	})
}

func TestSlotName(t *testing.T) {
	t.Parallel()

	t.Run("valid positions", func(t *testing.T) {
		t.Parallel()
		for i := range dispatcher.MaxPrograms {
			name, err := dispatcher.SlotName(i)
			if err != nil {
				t.Fatalf("SlotName(%d): unexpected error: %v", i, err)
			}
			want := "prog" + string(rune('0'+i))
			if name != want {
				t.Errorf("SlotName(%d) = %q, want %q", i, name, want)
			}
		}
	})

	t.Run("negative", func(t *testing.T) {
		t.Parallel()
		if _, err := dispatcher.SlotName(-1); err == nil {
			t.Error("SlotName(-1): expected error")
		}
	})

	t.Run("at max", func(t *testing.T) {
		t.Parallel()
		if _, err := dispatcher.SlotName(dispatcher.MaxPrograms); err == nil {
			t.Errorf("SlotName(%d): expected error", dispatcher.MaxPrograms)
		}
	})

	t.Run("above max", func(t *testing.T) {
		t.Parallel()
		if _, err := dispatcher.SlotName(100); err == nil {
			t.Error("SlotName(100): expected error")
		}
	})
}

func TestProceedOnMask(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		dt    dispatcher.DispatcherType
		codes []int32
		want  uint32
	}{
		{"tc unspec", dispatcher.DispatcherTypeTCIngress, []int32{-1}, 1 << 0},
		{"tc ok", dispatcher.DispatcherTypeTCIngress, []int32{0}, 1 << 1},
		{"tc pipe", dispatcher.DispatcherTypeTCIngress, []int32{3}, 1 << 4},
		{"tc dispatcher return", dispatcher.DispatcherTypeTCIngress, []int32{30}, 1 << 31},
		{"tc default", dispatcher.DispatcherTypeTCIngress, []int32{3, 30}, (1 << 4) | (1 << 31)},
		{"xdp pass", dispatcher.DispatcherTypeXDP, []int32{2}, 1 << 2},
		{"xdp dispatcher return", dispatcher.DispatcherTypeXDP, []int32{31}, 1 << 31},
		{"xdp default", dispatcher.DispatcherTypeXDP, []int32{2, 31}, (1 << 2) | (1 << 31)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := dispatcher.ProceedOnMask(tt.dt, tt.codes...)
			if err != nil {
				t.Fatalf("ProceedOnMask(%s, %v): %v", tt.dt, tt.codes, err)
			}
			if got != tt.want {
				t.Errorf("ProceedOnMask(%s, %v) = 0x%x, want 0x%x", tt.dt, tt.codes, got, tt.want)
			}
		})
	}
}

func TestProceedOnMaskRejectsInvalidActions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		dt   dispatcher.DispatcherType
		code int32
	}{
		{"tc unknown in range", dispatcher.DispatcherTypeTCIngress, 9},
		{"tc out of range", dispatcher.DispatcherTypeTCIngress, 99},
		{"xdp unknown in range", dispatcher.DispatcherTypeXDP, 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := dispatcher.ProceedOnMask(tt.dt, tt.code); err == nil {
				t.Fatalf("ProceedOnMask(%s, %d): expected error", tt.dt, tt.code)
			}
		})
	}
}

func TestProceedOnActions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		dt   dispatcher.DispatcherType
		mask uint32
		want []int32
	}{
		{"tc unspec", dispatcher.DispatcherTypeTCIngress, 1 << 0, []int32{-1}},
		{"tc default", dispatcher.DispatcherTypeTCIngress, (1 << 4) | (1 << 31), []int32{3, 30}},
		{"xdp default", dispatcher.DispatcherTypeXDP, (1 << 2) | (1 << 31), []int32{2, 31}},
		{"xdp explicit pass", dispatcher.DispatcherTypeXDP, 1 << 2, []int32{2}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := dispatcher.ProceedOnActions(tt.dt, tt.mask)
			if err != nil {
				t.Fatalf("ProceedOnActions(%s, 0x%x): %v", tt.dt, tt.mask, err)
			}
			if fmt.Sprint(got) != fmt.Sprint(tt.want) {
				t.Fatalf("ProceedOnActions(%s, 0x%x) = %v, want %v", tt.dt, tt.mask, got, tt.want)
			}
		})
	}
}

func TestProceedOnActionsRejectsInvalidBits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		dt   dispatcher.DispatcherType
		mask uint32
	}{
		{"tc bit ten", dispatcher.DispatcherTypeTCIngress, 1 << 10},
		{"xdp bit five", dispatcher.DispatcherTypeXDP, 1 << 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := dispatcher.ProceedOnActions(tt.dt, tt.mask); err == nil {
				t.Fatalf("ProceedOnActions(%s, 0x%x): expected error", tt.dt, tt.mask)
			}
		})
	}
}

func TestParseDispatcherType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input   string
		want    dispatcher.DispatcherType
		wantErr bool
	}{
		{"xdp", dispatcher.DispatcherTypeXDP, false},
		{"tc-ingress", dispatcher.DispatcherTypeTCIngress, false},
		{"tc-egress", dispatcher.DispatcherTypeTCEgress, false},
		{"", dispatcher.DispatcherType{}, true},
		{"XDP", dispatcher.DispatcherType{}, true},
		{"tc_ingress", dispatcher.DispatcherType{}, true},
		{"unknown", dispatcher.DispatcherType{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got, err := dispatcher.ParseDispatcherType(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseDispatcherType(%q): err = %v, wantErr = %v", tt.input, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("ParseDispatcherType(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestUnmarshalText(t *testing.T) {
	t.Parallel()

	t.Run("valid", func(t *testing.T) {
		t.Parallel()
		var dt dispatcher.DispatcherType
		if err := dt.UnmarshalText([]byte("xdp")); err != nil {
			t.Fatalf("UnmarshalText(xdp): %v", err)
		}
		if dt != dispatcher.DispatcherTypeXDP {
			t.Errorf("got %v, want %v", dt, dispatcher.DispatcherTypeXDP)
		}
	})

	t.Run("invalid", func(t *testing.T) {
		t.Parallel()
		var dt dispatcher.DispatcherType
		if err := dt.UnmarshalText([]byte("bogus")); err == nil {
			t.Error("UnmarshalText(bogus): expected error")
		}
	})
}

func TestTCParentHandle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		dt   dispatcher.DispatcherType
		want uint32
	}{
		{dispatcher.DispatcherTypeTCIngress, netlink.HANDLE_MIN_INGRESS},
		{dispatcher.DispatcherTypeTCEgress, netlink.HANDLE_MIN_EGRESS},
		{dispatcher.DispatcherTypeXDP, 0},
	}
	for _, tt := range tests {
		t.Run(tt.dt.String(), func(t *testing.T) {
			t.Parallel()
			if got := dispatcher.TCParentHandle(tt.dt); got != tt.want {
				t.Errorf("TCParentHandle(%s) = 0x%x, want 0x%x", tt.dt, got, tt.want)
			}
		})
	}
}

func TestXDPExtensionAttachSpecValidate(t *testing.T) {
	t.Parallel()

	valid := dispatcher.XDPExtensionAttachSpec{
		DispatcherPinPath: "/disp",
		ProgPinPath:       "/prog",
		ProgramName:       "test",
		Position:          0,
	}

	t.Run("valid at 0", func(t *testing.T) {
		t.Parallel()
		if err := valid.Validate(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("valid at max-1", func(t *testing.T) {
		t.Parallel()
		s := valid
		s.Position = dispatcher.MaxPrograms - 1
		if err := s.Validate(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("negative position", func(t *testing.T) {
		t.Parallel()
		s := valid
		s.Position = -1
		if err := s.Validate(); err == nil {
			t.Error("expected error for Position=-1")
		}
	})

	t.Run("position at max", func(t *testing.T) {
		t.Parallel()
		s := valid
		s.Position = dispatcher.MaxPrograms
		if err := s.Validate(); err == nil {
			t.Error("expected error for Position=MaxPrograms")
		}
	})
}

func TestTCExtensionAttachSpecValidate(t *testing.T) {
	t.Parallel()

	valid := dispatcher.TCExtensionAttachSpec{
		DispatcherPinPath: "/disp",
		ProgPinPath:       "/prog",
		ProgramName:       "test",
		Position:          0,
	}

	t.Run("valid at 0", func(t *testing.T) {
		t.Parallel()
		if err := valid.Validate(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("negative position", func(t *testing.T) {
		t.Parallel()
		s := valid
		s.Position = -1
		if err := s.Validate(); err == nil {
			t.Error("expected error for Position=-1")
		}
	})

	t.Run("position at max", func(t *testing.T) {
		t.Parallel()
		s := valid
		s.Position = dispatcher.MaxPrograms
		if err := s.Validate(); err == nil {
			t.Error("expected error for Position=MaxPrograms")
		}
	})
}
