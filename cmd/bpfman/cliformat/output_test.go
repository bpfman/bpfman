package cliformat

import (
	"testing"
)

func TestOutputFlags_Format(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		output  string
		want    OutputFormat
		wantErr bool
	}{
		{
			name:   "text",
			output: "text",
			want:   OutputFormatText,
		},
		{
			name:   "json",
			output: "json",
			want:   OutputFormatJSON,
		},
		{
			name:    "unknown format",
			output:  "xml",
			wantErr: true,
		},
		{
			name:    "empty",
			output:  "",
			wantErr: true,
		},
		{
			name:    "custom-columns no longer supported",
			output:  "custom-columns=ID:.record.program_id",
			wantErr: true,
		},
		{
			name:    "custom-columns-file no longer supported",
			output:  "custom-columns-file=/path/to/file.txt",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := &OutputFlags{Output: tt.output}
			got, err := f.Format()
			if (err != nil) != tt.wantErr {
				t.Errorf("Format() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("Format() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOutputFormat_NeedsLinkGetProgramName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		format OutputFormat
		want   bool
	}{
		{
			name:   "text",
			format: OutputFormatText,
			want:   true,
		},
		{
			name:   "json",
			format: OutputFormatJSON,
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.format.NeedsLinkGetProgramName()
			if got != tt.want {
				t.Errorf("NeedsLinkGetProgramName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOutputFormat_IsStructured(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		format OutputFormat
		want   bool
	}{
		{
			name:   "text",
			format: OutputFormatText,
			want:   false,
		},
		{
			name:   "json",
			format: OutputFormatJSON,
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.format.IsStructured(); got != tt.want {
				t.Errorf("IsStructured() = %v, want %v", got, tt.want)
			}
		})
	}
}
