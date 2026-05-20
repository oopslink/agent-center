package cli

import "os"

func writeFileImpl(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}
