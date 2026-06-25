package secret

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func InstallAgeKey(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("input key file does not exist: %s: %w", src, err)
	}
	if info.IsDir() {
		return fmt.Errorf("input key file is a directory: %s", src)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o400)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chmod(dst, 0o400)
}
