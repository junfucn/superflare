package editor

import (
	"archive/zip"
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"testing"

	"github.com/junfuchang/superflare/config/data"
	"github.com/junfuchang/superflare/config/model"
	"github.com/junfuchang/superflare/internal/background"
	"github.com/stretchr/testify/require"
)

func backupTestImage(t *testing.T, fill color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.SetNRGBA(x, y, fill)
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

func prepareBackupTest(t *testing.T) []byte {
	t.Helper()
	t.Chdir(t.TempDir())
	require.NoError(t, data.EnsureAppConfigExists())
	require.NoError(t, data.EnsureRuntimeDataFiles())
	raw := backupTestImage(t, color.NRGBA{R: 200, A: 255})
	_, err := background.PrepareUploadedBackground("original.png", bytes.NewReader(raw))
	require.NoError(t, err)
	require.NoError(t, data.UpdateThemeAndBackgroundSettings(model.Application{
		Theme: "blackboard", BackgroundImage: background.UploadedFullPath, BackgroundImageMode: "upload", BackgroundOpacity: 100,
	}))
	return raw
}

func TestBackupRestoresUploadedBackgroundWithoutOriginalFiles(t *testing.T) {
	original := prepareBackupTest(t)
	archive, err := buildBackupArchive()
	require.NoError(t, err)
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	require.NoError(t, err)
	f, err := zr.Open("uploads/background-source.png")
	require.NoError(t, err)
	imageData, err := io.ReadAll(f)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	require.Equal(t, original, imageData)
	require.NoError(t, background.DeleteUploadedBackgrounds())
	require.NoError(t, data.UpdateThemeAndBackgroundSettings(model.Application{Theme: "white"}))
	require.NoError(t, restoreZip(archive))
	options, err := data.GetAllSettingsOptions()
	require.NoError(t, err)
	require.True(t, background.UsesUploadedBackground(options))
	require.NoError(t, background.CheckUploadedBackground())
	_, restored, err := background.ReadUploadedBackgroundSource()
	require.NoError(t, err)
	require.Equal(t, original, restored)
}

func TestRestoreMissingBackgroundPreservesCurrentConfig(t *testing.T) {
	prepareBackupTest(t)
	rawConfig, err := os.ReadFile("config.yml")
	require.NoError(t, err)
	require.NoError(t, background.DeleteUploadedBackgrounds())
	require.NoError(t, data.UpdateThemeAndBackgroundSettings(model.Application{Theme: "white"}))
	before, err := os.ReadFile("config.yml")
	require.NoError(t, err)
	for _, zipped := range []bool{false, true} {
		if zipped {
			var archive bytes.Buffer
			zw := zip.NewWriter(&archive)
			entry, err := zw.Create("config.yml")
			require.NoError(t, err)
			_, err = entry.Write(rawConfig)
			require.NoError(t, err)
			require.NoError(t, zw.Close())
			err = restoreZip(archive.Bytes())
			require.ErrorContains(t, err, "missing uploaded background")
		} else {
			err = writeRestoreFilesAtomically(map[string][]byte{"config": rawConfig})
			require.ErrorContains(t, err, "missing uploaded background")
		}
		after, err := os.ReadFile("config.yml")
		require.NoError(t, err)
		require.Equal(t, before, after)
	}
}

func TestBackgroundRestoreRollsBackImageAndConfigTogether(t *testing.T) {
	prepareBackupTest(t)
	archive, err := buildBackupArchive()
	require.NoError(t, err)
	currentImage := backupTestImage(t, color.NRGBA{B: 220, A: 255})
	_, err = background.PrepareUploadedBackground("current.png", bytes.NewReader(currentImage))
	require.NoError(t, err)
	require.NoError(t, data.UpdateThemeName("white"))
	before, err := os.ReadFile("config.yml")
	require.NoError(t, err)
	originalRefresh := refreshPagePaletteCache
	refreshPagePaletteCache = func() error { return errors.New("test refresh failure") }
	t.Cleanup(func() { refreshPagePaletteCache = originalRefresh })
	require.ErrorContains(t, restoreZip(archive), "test refresh failure")
	after, err := os.ReadFile("config.yml")
	require.NoError(t, err)
	require.Equal(t, before, after)
	_, imageData, err := background.ReadUploadedBackgroundSource()
	require.NoError(t, err)
	require.Equal(t, currentImage, imageData)
}

func TestRestoreRejectsInvalidAndDuplicateBackgrounds(t *testing.T) {
	original := prepareBackupTest(t)
	rawConfig, err := os.ReadFile("config.yml")
	require.NoError(t, err)
	for _, duplicate := range []bool{false, true} {
		var archive bytes.Buffer
		zw := zip.NewWriter(&archive)
		entry, err := zw.Create("config.yml")
		require.NoError(t, err)
		_, err = entry.Write(rawConfig)
		require.NoError(t, err)
		names := []string{"uploads/background-source.png"}
		if duplicate {
			names = append(names, "uploads/background-source.jpg")
		}
		for _, name := range names {
			entry, err := zw.Create(name)
			require.NoError(t, err)
			_, err = entry.Write([]byte("not an image"))
			require.NoError(t, err)
		}
		require.NoError(t, zw.Close())
		require.Error(t, restoreZip(archive.Bytes()))
		_, raw, err := background.ReadUploadedBackgroundSource()
		require.NoError(t, err)
		require.Equal(t, original, raw)
	}
}
