package qmk

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Where Read fetches keyboard definitions named rather than given as a file.
const (
	apiURL = "https://keyboards.qmk.fm/v1/keyboards/"
	rawURL = "https://raw.githubusercontent.com/qmk/qmk_firmware/master/keyboards/"
)

// Client fetches keyboard definitions for Read; the timeout keeps it from
// hanging.
var Client = &http.Client{Timeout: 15 * time.Second}

// localPath reports whether src names a file rather than a keyboard in
// qmk_firmware, so a mistyped path isn't sent to QMK and GitHub.
func localPath(src string) bool {
	return filepath.IsAbs(src) || strings.HasPrefix(src, ".") || strings.HasPrefix(src, "~") || strings.HasSuffix(src, ".json")
}

// Read reads a keyboard: src is a keyboard.json file, or a keyboard's path
// in qmk_firmware/keyboards (zsa/voyager) to fetch from QMK.
func Read(src string) (Keyboard, error) {
	f, err := os.Open(src)
	if err == nil {
		defer f.Close()
		return Parse(f)
	}
	if !errors.Is(err, fs.ErrNotExist) || localPath(src) {
		return Keyboard{}, err
	}
	var errs []error
	// QMK's API has the merged definition, which variants inherit; the raw
	// files are a fallback
	urls := []string{apiURL + strings.Trim(src, "/") + "/info.json"}
	for _, file := range []string{"keyboard.json", "info.json"} {
		urls = append(urls, rawURL+strings.Trim(src, "/")+"/"+file)
	}
	for _, url := range urls {
		resp, err := Client.Get(url)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			errs = append(errs, fmt.Errorf("%s: %s", url, resp.Status))
			continue
		}
		kb, err := Parse(resp.Body)
		resp.Body.Close()
		return kb, err
	}
	return Keyboard{}, fmt.Errorf("%s is not a file or a QMK keyboard: %w", src, errors.Join(errs...))
}
