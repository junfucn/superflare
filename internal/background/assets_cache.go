package background

import (
	"os"
	"path/filepath"
	"sync"
)

const previewMetadataCacheLimit = 16

type variantStamp struct {
	data os.FileInfo
	kind os.FileInfo
}

func statVariant(prefix string) (variantStamp, error) {
	data, err := os.Stat(prefix + ".bin")
	if err != nil {
		return variantStamp{}, err
	}
	kind, err := os.Stat(prefix + ".type")
	if err != nil {
		return variantStamp{}, err
	}
	if !data.Mode().IsRegular() || !kind.Mode().IsRegular() || data.Size() == 0 || kind.Size() == 0 {
		return variantStamp{}, os.ErrNotExist
	}
	return variantStamp{data: data, kind: kind}, nil
}

func sameVariantStamp(a, b variantStamp) bool {
	sameFile := func(a, b os.FileInfo) bool {
		return a != nil && b != nil && os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
	}
	return sameFile(a.data, b.data) && sameFile(a.kind, b.kind)
}

type previewMetadata struct {
	stamp   variantStamp
	dataURL string
	accent  string
}

var previewMetadataCache = struct {
	sync.Mutex
	entries map[string]previewMetadata
}{entries: make(map[string]previewMetadata)}

func resolvePreviewMetadata(prefix string, loader func() ([]byte, string, error)) (string, string) {
	previewMetadataCache.Lock()
	defer previewMetadataCache.Unlock()

	stamp, statErr := statVariant(prefix)
	if cached, ok := previewMetadataCache.entries[prefix]; ok && statErr == nil && sameVariantStamp(cached.stamp, stamp) {
		return cached.dataURL, cached.accent
	}
	delete(previewMetadataCache.entries, prefix)
	data, contentType, err := loader()
	if err != nil {
		return "", ""
	}
	load := func() ([]byte, string, error) { return data, contentType, nil }
	metadata := previewMetadata{
		stamp:   stamp,
		dataURL: buildInlinePreviewDataURL(load),
		accent:  detectAccentColor(load),
	}
	// Do not cache a preview that was missing or replaced while being read.
	if current, err := statVariant(prefix); statErr == nil && err == nil && sameVariantStamp(stamp, current) {
		if len(previewMetadataCache.entries) >= previewMetadataCacheLimit {
			for key := range previewMetadataCache.entries {
				delete(previewMetadataCache.entries, key)
				break
			}
		}
		previewMetadataCache.entries[prefix] = metadata
	}
	return metadata.dataURL, metadata.accent
}

func CheckUploadedBackground() error {
	if err := EnsureUploadedBackgroundVariants(); err != nil {
		return err
	}
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	dir := filepath.Join(root, uploadDir)
	if _, err := statVariant(filepath.Join(dir, "background-full")); err == nil {
		return nil
	}
	source, err := findUploadedSourceFile(dir)
	if err != nil {
		return err
	}
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return os.ErrNotExist
	}
	return nil
}
