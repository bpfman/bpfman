package server

import (
	"testing"

	pb "github.com/bpfman/bpfman/server/pb"
)

func TestProtoImageAuthRequiresBothCredentials(t *testing.T) {
	t.Parallel()

	username := "user"
	password := "pass"
	empty := ""

	cases := []struct {
		name string
		img  *pb.BytecodeImage
		want bool
	}{
		{"nil image", nil, false},
		{"missing password", &pb.BytecodeImage{Username: &username}, false},
		{"missing username", &pb.BytecodeImage{Password: &password}, false},
		{"empty password", &pb.BytecodeImage{Username: &username, Password: &empty}, false},
		{"empty username", &pb.BytecodeImage{Username: &empty, Password: &password}, false},
		{"both present", &pb.BytecodeImage{Username: &username, Password: &password}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := protoImageAuth(tc.img)
			if (got != nil) != tc.want {
				t.Fatalf("protoImageAuth present = %v, want %v", got != nil, tc.want)
			}
		})
	}
}
