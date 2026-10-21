// Command spike evaluates pure-Go metadata libraries (ADR-001, task T0.3).
//
// Usage: spike [-timeout 5s] <corpus-dir>
//
// Every file in the corpus is run through every applicable library. Each run
// is isolated: panics are recovered and a timeout is enforced, which doubles
// as a crude robustness check (S5). Output is a TSV table on stdout.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	mp4 "github.com/abema/go-mp4"
	goexif3 "github.com/dsoprea/go-exif/v3"
	"github.com/evanoberholster/imagemeta"
	rwexif "github.com/rwcarlsen/goexif/exif"
)

// result is what one library extracted from one file.
type result struct {
	Date  string // formatted capture time, empty if none
	Zone  string // "offset" if the lib reported an offset/zone, "naive" if not
	Make  string
	Model string
	Err   string
}

type extractor struct {
	name string
	run  func(path string) result
}

func main() {
	timeout := flag.Duration("timeout", 5*time.Second, "per-file, per-library timeout")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: spike [-timeout d] <corpus-dir>")
		os.Exit(2)
	}
	entries, err := os.ReadDir(flag.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var files []string
	for _, e := range entries {
		if e.Type().IsRegular() {
			files = append(files, filepath.Join(flag.Arg(0), e.Name()))
		}
	}
	sort.Strings(files)

	exs := []extractor{
		{"imagemeta", runImagemeta},
		{"rwcarlsen/goexif", runRwcarlsen},
		{"dsoprea/go-exif", runDsoprea},
		{"abema/go-mp4", runMP4},
	}
	fmt.Println("file\tlib\tdate\tzone\tmake\tmodel\tms\terror")
	for _, f := range files {
		for _, ex := range exs {
			start := time.Now()
			r := guarded(ex.run, f, *timeout)
			fmt.Printf("%s\t%s\t%s\t%s\t%s\t%s\t%d\t%s\n",
				filepath.Base(f), ex.name, r.Date, r.Zone, clean(r.Make), clean(r.Model),
				time.Since(start).Milliseconds(), clean(r.Err))
		}
	}
}

func clean(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	if len(s) > 80 {
		s = s[:80]
	}
	return s
}

// guarded runs fn with panic recovery and a timeout.
func guarded(fn func(string) result, path string, timeout time.Duration) result {
	ch := make(chan result, 1)
	go func() {
		defer func() {
			if p := recover(); p != nil {
				ch <- result{Err: fmt.Sprintf("PANIC: %v", p)}
			}
		}()
		ch <- fn(path)
	}()
	select {
	case r := <-ch:
		return r
	case <-time.After(timeout):
		return result{Err: "TIMEOUT"}
	}
}

func zoneKind(t time.Time) string {
	if t.Location() == time.UTC {
		return "naive(stamped UTC)"
	}
	return "offset"
}

func runImagemeta(path string) result {
	f, err := os.Open(path)
	if err != nil {
		return result{Err: err.Error()}
	}
	defer f.Close()
	ex, err := imagemeta.Decode(f)
	r := result{Make: ex.CameraMake(), Model: ex.IFD0.Model}
	if t := ex.OriginalDate(); !t.IsZero() {
		r.Date, r.Zone = t.Format(time.RFC3339), zoneKind(t)
		if ex.ExifIFD.OffsetTimeOriginal != nil {
			r.Zone = "offset(OffsetTimeOriginal)"
		}
	} else if t := ex.SelectedDate(); !t.IsZero() {
		r.Date, r.Zone = t.Format(time.RFC3339), zoneKind(t)
	}
	if err != nil {
		r.Err = err.Error()
	}
	return r
}

func runRwcarlsen(path string) result {
	f, err := os.Open(path)
	if err != nil {
		return result{Err: err.Error()}
	}
	defer f.Close()
	x, err := rwexif.Decode(f)
	if err != nil {
		return result{Err: err.Error()}
	}
	var r result
	if t, err := x.DateTime(); err == nil {
		// goexif stamps naive times with time.Local (the host zone), so the
		// offset is meaningless; only the wall clock is trustworthy (D9).
		r.Date, r.Zone = t.Format("2006-01-02T15:04:05"), "naive(host-local stamped)"
	}
	if tag, err := x.Get(rwexif.Make); err == nil {
		r.Make, _ = tag.StringVal()
	}
	if tag, err := x.Get(rwexif.Model); err == nil {
		r.Model, _ = tag.StringVal()
	}
	return r
}

func runDsoprea(path string) result {
	data, err := os.ReadFile(path)
	if err != nil {
		return result{Err: err.Error()}
	}
	raw, err := goexif3.SearchAndExtractExifWithReader(bytes.NewReader(data))
	if err != nil {
		return result{Err: err.Error()}
	}
	tags, _, err := goexif3.GetFlatExifData(raw, nil)
	if err != nil {
		return result{Err: err.Error()}
	}
	var r result
	var orig, create, mod string
	for _, t := range tags {
		switch t.TagName {
		case "DateTimeOriginal":
			orig = t.FormattedFirst
		case "DateTimeDigitized":
			create = t.FormattedFirst
		case "DateTime":
			mod = t.FormattedFirst
		case "Make":
			r.Make = t.FormattedFirst
		case "Model":
			r.Model = t.FormattedFirst
		}
	}
	for _, s := range []string{orig, create, mod} {
		if s != "" {
			if t, err := time.Parse("2006:01:02 15:04:05", s); err == nil {
				r.Date, r.Zone = t.Format("2006-01-02T15:04:05"), "naive"
				break
			}
		}
	}
	return r
}

// runMP4 reads mvhd creation_time (UTC, 1904 epoch) via abema/go-mp4.
func runMP4(path string) result {
	f, err := os.Open(path)
	if err != nil {
		return result{Err: err.Error()}
	}
	defer f.Close()
	var r result
	_, err = mp4.ReadBoxStructure(f, func(h *mp4.ReadHandle) (any, error) {
		switch h.BoxInfo.Type {
		case mp4.BoxTypeMoov():
			return h.Expand()
		case mp4.BoxTypeMvhd():
			box, _, err := h.ReadPayload()
			if err != nil {
				return nil, err
			}
			m := box.(*mp4.Mvhd)
			secs := uint64(m.CreationTimeV0)
			if m.GetVersion() == 1 {
				secs = m.CreationTimeV1
			}
			t := time.Date(1904, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(secs) * time.Second)
			r.Date, r.Zone = t.Format(time.RFC3339), "utc-only"
		}
		return nil, nil
	})
	if err != nil && err != io.EOF {
		r.Err = err.Error()
	}
	return r
}
