package dispatcher_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bpfman/bpfman/dispatcher"
)

func TestKeyFilterMatches(t *testing.T) {
	t.Parallel()

	key := dispatcher.NewKey(dispatcher.DispatcherTypeTCIngress, 4026531840, 7)

	tests := []struct {
		name   string
		filter dispatcher.KeyFilter
		want   bool
	}{
		{name: "zero filter matches everything", want: true},
		{
			name:   "matching type",
			filter: dispatcher.KeyFilter{Type: dispatcher.DispatcherTypeTCIngress},
			want:   true,
		},
		{
			name:   "mismatching type",
			filter: dispatcher.KeyFilter{Type: dispatcher.DispatcherTypeXDP},
			want:   false,
		},
		{
			name:   "matching nsid",
			filter: dispatcher.KeyFilter{Nsid: 4026531840},
			want:   true,
		},
		{
			name:   "mismatching nsid",
			filter: dispatcher.KeyFilter{Nsid: 1},
			want:   false,
		},
		{
			name:   "matching ifindex",
			filter: dispatcher.KeyFilter{Ifindex: 7},
			want:   true,
		},
		{
			name:   "mismatching ifindex",
			filter: dispatcher.KeyFilter{Ifindex: 8},
			want:   false,
		},
		{
			name: "all fields match",
			filter: dispatcher.KeyFilter{
				Type:    dispatcher.DispatcherTypeTCIngress,
				Nsid:    4026531840,
				Ifindex: 7,
			},
			want: true,
		},
		{
			name: "one field mismatches among matches",
			filter: dispatcher.KeyFilter{
				Type:    dispatcher.DispatcherTypeTCIngress,
				Nsid:    4026531840,
				Ifindex: 8,
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.filter.Matches(key))
		})
	}
}
