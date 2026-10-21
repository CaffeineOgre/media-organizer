package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	mp4 "github.com/abema/go-mp4"
	goexif3 "github.com/dsoprea/go-exif/v3"
	"github.com/evanoberholster/imagemeta"
	rwexif "github.com/rwcarlsen/goexif/exif"
)

// seed adds every corpus file under 200 KB to f.
func seed(f *testing.F) {
	entries, _ := os.ReadDir(corpus)
	for _, e := range entries {
		p := filepath.Join(corpus, e.Name())
		if st, err := os.Stat(p); err == nil && st.Size() < 200<<10 {
			if b, err := os.ReadFile(p); err == nil {
				f.Add(b)
			}
		}
	}
}

func FuzzImagemeta(f *testing.F) {
	seed(f)
	f.Fuzz(func(t *testing.T, b []byte) {
		done := make(chan struct{})
		go func() {
			defer close(done)
			defer func() { _ = recover() }()
			_, _ = imagemeta.Decode(bytes.NewReader(b))
		}()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatalf("imagemeta.Decode still running after 3s on %d-byte input", len(b))
		}
	})
}

func FuzzRwcarlsen(f *testing.F) {
	seed(f)
	f.Fuzz(func(t *testing.T, b []byte) {
		if x, err := rwexif.Decode(bytes.NewReader(b)); err == nil {
			_, _ = x.DateTime()
		}
	})
}

func FuzzDsoprea(f *testing.F) {
	seed(f)
	f.Fuzz(func(t *testing.T, b []byte) {
		raw, err := goexif3.SearchAndExtractExifWithReader(bytes.NewReader(b))
		if err != nil {
			return
		}
		_, _, _ = goexif3.GetFlatExifData(raw, nil)
	})
}

func FuzzGoMP4(f *testing.F) {
	seed(f)
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = mp4.ReadBoxStructure(bytes.NewReader(b), func(h *mp4.ReadHandle) (any, error) {
			if h.BoxInfo.Type == mp4.BoxTypeMoov() {
				return h.Expand()
			}
			if h.BoxInfo.Type == mp4.BoxTypeMvhd() {
				_, _, err := h.ReadPayload()
				return nil, err
			}
			_, _ = io.Discard, 0
			return nil, nil
		})
	})
}
