package ns

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// MaxNameLen is the maximum length of the name sent with a file descriptor.
const MaxNameLen = 4096

// oobSpace is the size of the oob slice required to store a single FD.
var oobSpace = unix.CmsgSpace(4)

// SendFd sends a file descriptor over a Unix socket.
// The name is sent as regular data alongside the fd.
func SendFd(socket *os.File, name string, fd int) error {
	if len(name) >= MaxNameLen {
		return fmt.Errorf("sendfd: name too long: %d >= %d", len(name), MaxNameLen)
	}

	oob := unix.UnixRights(fd)
	sockfd := int(socket.Fd())

	for {
		err := unix.Sendmsg(sockfd, []byte(name), oob, nil, 0)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return fmt.Errorf("sendmsg: %w", err)
		}
		return nil
	}
}

// RecvFd receives a file descriptor from a Unix socket.
// Returns the received fd number and the name sent with it.
func RecvFd(socket *os.File) (fd int, name string, err error) {
	nameBuf := make([]byte, MaxNameLen)
	oob := make([]byte, oobSpace)
	sockfd := int(socket.Fd())

	var n, oobn int
	for {
		n, oobn, _, _, err = unix.Recvmsg(sockfd, nameBuf, oob, unix.MSG_CMSG_CLOEXEC)
		if err == unix.EINTR {
			continue
		}
		break
	}

	if err != nil {
		return -1, "", fmt.Errorf("recvmsg: %w", err)
	}

	if n >= MaxNameLen {
		return -1, "", fmt.Errorf("recvfd: name too long: %d", n)
	}
	if oobn != oobSpace {
		return -1, "", fmt.Errorf("recvfd: unexpected oob length: got %d, want %d", oobn, oobSpace)
	}

	nameBuf = nameBuf[:n]
	oob = oob[:oobn]

	scms, err := unix.ParseSocketControlMessage(oob)
	if err != nil {
		return -1, "", fmt.Errorf("parse control message: %w", err)
	}

	if len(scms) != 1 {
		return -1, "", fmt.Errorf("recvfd: expected 1 SCM, got %d", len(scms))
	}

	fds, err := unix.ParseUnixRights(&scms[0])
	if err != nil {
		return -1, "", fmt.Errorf("parse unix rights: %w", err)
	}

	if len(fds) != 1 {
		// Close any extra fds we received
		for _, extraFd := range fds {
			unix.Close(extraFd)
		}
		return -1, "", fmt.Errorf("recvfd: expected 1 fd, got %d", len(fds))
	}

	return fds[0], string(nameBuf), nil
}

// Socketpair creates a pair of connected Unix sockets.
// Returns (parent socket, child socket, error).
func Socketpair() (parent, child *os.File, err error) {
	fds, err := unix.Socketpair(unix.AF_LOCAL, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("socketpair: %w", err)
	}

	parent = os.NewFile(uintptr(fds[0]), "fdpass-parent")
	child = os.NewFile(uintptr(fds[1]), "fdpass-child")
	return parent, child, nil
}
