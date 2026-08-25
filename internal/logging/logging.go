package logging

import (
	"io"
	"log"
	"os"
	"path/filepath"
)

func New(dataDir string) (*log.Logger, io.Closer, error) {
	directory := filepath.Join(dataDir, "logs")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return nil, nil, err
	}
	path := filepath.Join(directory, "foldersnap.log")
	if stat, err := os.Stat(path); err == nil && stat.Size() > 5<<20 {
		_ = os.Remove(path + ".1")
		_ = os.Rename(path, path+".1")
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, nil, err
	}
	return log.New(file, "", log.Ldate|log.Ltime|log.LUTC|log.Lmsgprefix), file, nil
}
