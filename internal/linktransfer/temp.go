package linktransfer

import (
	"os"
	"path/filepath"
)

func writeTempTextFile(text string) (string, error) {
	f, err := os.CreateTemp(".", "linktransfer-text-")
	if err != nil {
		return "", err
	}
	name := f.Name()
	_, werr := f.WriteString(text)
	cerr := f.Close()
	if werr != nil {
		os.Remove(name)
		return "", werr
	}
	if cerr != nil {
		os.Remove(name)
		return "", cerr
	}
	return filepath.Clean(name), nil
}
