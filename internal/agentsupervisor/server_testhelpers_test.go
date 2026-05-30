package agentsupervisor_test

import (
	"net"
	"os"
	"testing"
)

// statSize returns the on-disk byte size of path.
func statSize(t *testing.T, path string) int64 {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return fi.Size()
}

// dialRaw opens a raw unix connection to the supervisor socket (used to send a
// hand-crafted oversize frame the AttachClient would never produce).
func dialRaw(sock string) (net.Conn, error) {
	return net.Dial("unix", sock)
}
