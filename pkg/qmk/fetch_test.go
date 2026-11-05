package qmk

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// qmkFake answers QMK's URLs from a map of path suffix to body; others 404.
type qmkFake map[string]string

func (f qmkFake) RoundTrip(r *http.Request) (*http.Response, error) {
	for suffix, body := range f {
		if strings.HasSuffix(r.URL.String(), suffix) {
			if body == "" {
				return nil, errors.New("connection refused")
			}
			return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		}
	}
	return &http.Response{StatusCode: http.StatusNotFound, Status: "404 Not Found", Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
}

func TestRead(t *testing.T) {
	const pad = `{"keyboard_name":"pad","layouts":{"LAYOUT":{"layout":[{"matrix":[0,0],"x":0,"y":0}]}}}`
	file := filepath.Join(t.TempDir(), "pad.json")
	if err := os.WriteFile(file, []byte(pad), 0o644); err != nil {
		t.Fatal(err)
	}
	unreadable := filepath.Join(t.TempDir(), "locked")
	if err := os.Mkdir(unreadable, 0o000); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		src     string
		served  qmkFake
		want    string
		wantErr string
	}{
		{"a file", file, nil, "pad", ""},
		{"QMK's API", "acme/pad", qmkFake{"v1/keyboards/acme/pad/info.json": pad}, "pad", ""},
		{"raw keyboard.json when the API lacks it", "acme/pad/", qmkFake{"keyboards/acme/pad/keyboard.json": pad}, "pad", ""},
		{"raw files after a network error", "acme/pad", qmkFake{"v1/keyboards/acme/pad/info.json": "", "master/keyboards/acme/pad/info.json": pad}, "pad", ""},
		{"nowhere", "acme/none", qmkFake{}, "", "acme/none is not a file or a QMK keyboard"},
		{"a file that can't be read isn't fetched", filepath.Join(unreadable, "x.json"), nil, "", "permission denied"},
		{"a missing file isn't fetched", filepath.Join(t.TempDir(), "pad.json"), qmkFake{}, "", "no such file"},
		{"nor a relative path", "../acme/pad", qmkFake{}, "", "no such file"},
	}
	old := Client
	t.Cleanup(func() { Client = old })
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if os.Geteuid() == 0 && strings.Contains(tt.wantErr, "permission") {
				t.Skip("root reads anything")
			}
			Client = &http.Client{Transport: tt.served}
			kb, err := Read(tt.src)
			if tt.wantErr == "" && err != nil || tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("err = %v, want %q", err, tt.wantErr)
			}
			if kb.Name != tt.want {
				t.Errorf("keyboard %q, want %q", kb.Name, tt.want)
			}
		})
	}
}
