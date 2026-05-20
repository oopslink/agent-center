package shim

import "os"

func testSelfPID() int { return os.Getpid() }
