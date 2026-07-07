package args

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bpfman/bpfman"
)

func TestParseProgramSpec_ValidInputs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input              string
		expectedType       bpfman.ProgramType
		expectedName       string
		expectedAttachFunc string
	}{
		{"xdp:my_prog", bpfman.ProgramTypeXDP, "my_prog", ""},
		{"tc:tc_ingress", bpfman.ProgramTypeTC, "tc_ingress", ""},
		{"tcx:tcx_prog", bpfman.ProgramTypeTCX, "tcx_prog", ""},
		{"tracepoint:count_switches", bpfman.ProgramTypeTracepoint, "count_switches", ""},
		{"kprobe:probe_func", bpfman.ProgramTypeKprobe, "probe_func", ""},
		{"kretprobe:ret_probe", bpfman.ProgramTypeKretprobe, "ret_probe", ""},
		{"uprobe:user_probe", bpfman.ProgramTypeUprobe, "user_probe", ""},
		{"uretprobe:user_ret", bpfman.ProgramTypeUretprobe, "user_ret", ""},
		// fentry/fexit require attach function (TYPE:NAME:ATTACH_FUNC)
		{"fentry:entry_func:do_unlinkat", bpfman.ProgramTypeFentry, "entry_func", "do_unlinkat"},
		{"fexit:exit_func:do_unlinkat", bpfman.ProgramTypeFexit, "exit_func", "do_unlinkat"},
		// With whitespace
		{"  xdp:my_prog  ", bpfman.ProgramTypeXDP, "my_prog", ""},
		{"xdp:  my_prog", bpfman.ProgramTypeXDP, "my_prog", ""},
		{"  xdp  :  my_prog  ", bpfman.ProgramTypeXDP, "my_prog", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			spec, err := ParseProgramSpec(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expectedType, spec.Type)
			assert.Equal(t, tt.expectedName, spec.Name)
			assert.Equal(t, tt.expectedAttachFunc, spec.AttachFunc)
		})
	}
}

func TestParseProgramSpec_InvalidInputs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input       string
		errContains string
	}{
		{"", "cannot be empty"},
		{"  ", "cannot be empty"},
		{"xdp", "expected TYPE:NAME format"},
		{"my_prog", "expected TYPE:NAME format"},
		{":my_prog", "type cannot be empty"},
		{"xdp:", "name cannot be empty"},
		{":", "type cannot be empty"},
		{"invalid:my_prog", "unknown type \"invalid\""},
		{"INVALID:my_prog", "unknown type \"INVALID\""},
		{"XDP:my_prog", "unknown type \"XDP\""}, // case sensitive
		// fentry/fexit require attach function
		{"fentry:entry_func", "fentry requires attach function"},
		{"fexit:exit_func", "fexit requires attach function"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			_, err := ParseProgramSpec(tt.input)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errContains)
		})
	}
}

func TestParseKeyValue_ValidInputs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input         string
		expectedKey   string
		expectedValue string
	}{
		{"key=value", "key", "value"},
		{"owner=acme", "owner", "acme"},
		{"bpfman.io/application=stats", "bpfman.io/application", "stats"},
		{"key=", "key", ""},                                           // empty value is valid
		{"key=value=with=equals", "key", "value=with=equals"},         // value can contain =
		{"  key  =value", "key", "value"},                             // whitespace in key trimmed
		{"key=  value with spaces  ", "key", "  value with spaces  "}, // value whitespace preserved
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			kv, err := ParseKeyValue(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expectedKey, kv.Key)
			assert.Equal(t, tt.expectedValue, kv.Value)
		})
	}
}

func TestParseKeyValue_InvalidInputs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input       string
		errContains string
	}{
		{"noequals", "expected KEY=VALUE"},
		{"=value", "expected KEY=VALUE"}, // empty key
		{"", "expected KEY=VALUE"},
		{"   =value", "key cannot be empty"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			_, err := ParseKeyValue(tt.input)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errContains)
		})
	}
}

func TestParseGlobalData_ValidInputs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input        string
		expectedName string
		expectedData []byte
	}{
		{"GLOBAL_u8=01", "GLOBAL_u8", []byte{0x01}},
		{"GLOBAL_u32=0A0B0C0D", "GLOBAL_u32", []byte{0x0A, 0x0B, 0x0C, 0x0D}},
		{"sampling=00000001", "sampling", []byte{0x00, 0x00, 0x00, 0x01}},
		// With 0x prefix
		{"GLOBAL_u8=0x01", "GLOBAL_u8", []byte{0x01}},
		{"GLOBAL_u8=0X01", "GLOBAL_u8", []byte{0x01}},
		{"GLOBAL_u32=0x0A0B0C0D", "GLOBAL_u32", []byte{0x0A, 0x0B, 0x0C, 0x0D}},
		// With whitespace
		{"  name  =  0x01  ", "name", []byte{0x01}},
		// Empty data
		{"empty=", "empty", []byte{}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			gd, err := ParseGlobalData(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expectedName, gd.Name)
			assert.Equal(t, tt.expectedData, gd.Data)
		})
	}
}

func TestParseGlobalData_InvalidInputs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input       string
		errContains string
	}{
		{"noequals", "expected NAME=HEX"},
		{"=01", "expected NAME=HEX"}, // empty name
		{"", "expected NAME=HEX"},
		{"   =01", "name cannot be empty"},
		{"name=GG", "invalid hex data"}, // invalid hex
		{"name=0xGG", "invalid hex data"},
		{"name=123", "invalid hex data"}, // odd number of hex chars
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			_, err := ParseGlobalData(tt.input)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errContains)
		})
	}
}

func TestMetadataMap(t *testing.T) {
	t.Parallel()

	t.Run("nil for empty slice", func(t *testing.T) {
		t.Parallel()
		result := MetadataMap(nil)
		assert.Nil(t, result)

		result = MetadataMap([]KeyValue{})
		assert.Nil(t, result)
	})

	t.Run("converts to map", func(t *testing.T) {
		t.Parallel()
		input := []KeyValue{
			{Key: "owner", Value: "acme"},
			{Key: "app", Value: "test"},
		}
		result := MetadataMap(input)
		assert.Equal(t, map[string]string{
			"owner": "acme",
			"app":   "test",
		}, result)
	})

	t.Run("last value wins for duplicate keys", func(t *testing.T) {
		t.Parallel()
		input := []KeyValue{
			{Key: "key", Value: "first"},
			{Key: "key", Value: "second"},
		}
		result := MetadataMap(input)
		assert.Equal(t, "second", result["key"])
	})
}

func TestGlobalDataMap(t *testing.T) {
	t.Parallel()

	t.Run("nil for empty slice", func(t *testing.T) {
		t.Parallel()
		result := GlobalDataMap(nil)
		assert.Nil(t, result)

		result = GlobalDataMap([]GlobalData{})
		assert.Nil(t, result)
	})

	t.Run("converts to map", func(t *testing.T) {
		t.Parallel()
		input := []GlobalData{
			{Name: "GLOBAL_u8", Data: []byte{0x01}},
			{Name: "GLOBAL_u32", Data: []byte{0x0A, 0x0B, 0x0C, 0x0D}},
		}
		result := GlobalDataMap(input)
		assert.Equal(t, map[string][]byte{
			"GLOBAL_u8":  {0x01},
			"GLOBAL_u32": {0x0A, 0x0B, 0x0C, 0x0D},
		}, result)
	})

	t.Run("last value wins for duplicate names", func(t *testing.T) {
		t.Parallel()
		input := []GlobalData{
			{Name: "var", Data: []byte{0x01}},
			{Name: "var", Data: []byte{0x02}},
		}
		result := GlobalDataMap(input)
		assert.Equal(t, []byte{0x02}, result["var"])
	})
}
