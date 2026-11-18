package data

import "fmt"

// MaxConfigFileSize keeps saved configuration compatible with backup restore.
const MaxConfigFileSize = 8 << 20

func ValidateConfigFileSize(name string, raw []byte) error {
	if len(raw) > MaxConfigFileSize {
		return fmt.Errorf("config file %s exceeds the %d MiB limit", name, MaxConfigFileSize>>20)
	}
	return nil
}
