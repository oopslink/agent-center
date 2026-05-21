package integration

import "os"

func osMkdirAll(dir string) error {
	return os.MkdirAll(dir, 0o755)
}

func osWriteFile(path string, b []byte) error {
	return os.WriteFile(path, b, 0o644)
}
