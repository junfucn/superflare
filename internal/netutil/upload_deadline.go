package netutil

import (
	"errors"
	"net/http"
	"time"
)

// Upload routes keep a bounded transfer window and leave time to process the
// body and send a response after the server's ordinary deadlines would expire.
func SetUploadDeadlines(w http.ResponseWriter) error {
	controller := http.NewResponseController(w)
	readDeadline := time.Now().Add(5 * time.Minute)
	if err := controller.SetReadDeadline(readDeadline); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return err
	}
	if err := controller.SetWriteDeadline(readDeadline.Add(30 * time.Second)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return err
	}
	return nil
}
