package main

import (
	"bytes"
	"os"
	"testing"
	"time"

	mp4 "github.com/abema/go-mp4"
	"github.com/evanoberholster/imagemeta"
	rwexif "github.com/rwcarlsen/goexif/exif"
)

// corpus is the sample directory; set SPIKE_CORPUS (see README.md).
var corpus = func() string {
	if d := os.Getenv("SPIKE_CORPUS"); d != "" {
		return d + "/"
	}
	return "corpus/"
}()

func TestProbeJPEG(t *testing.T) {
	f, _ := os.Open(corpus + "JPEG.jpg")
	defer f.Close()
	ex, err := imagemeta.Decode(f)
	t.Logf("err=%v make=%q model=%q", err, ex.IFD0.Make, ex.IFD0.Model)
	t.Logf("orig=%v create=%v selected=%v", ex.ExifIFD.DateTimeOriginal, ex.ExifIFD.CreateDate, ex.SelectedDate())
	t.Logf("ifd0 datetime=%v", ex.ModifyDate())
}

func TestProbeJPEG2(t *testing.T) {
	data, _ := os.ReadFile(corpus + "JPEG.jpg")
	ex, err := imagemeta.DecodeJPEG(bytes.NewReader(data))
	t.Logf("DecodeJPEG err=%v make=%q date=%v", err, ex.IFD0.Make, ex.SelectedDate())
	small := append([]byte{0xff, 0xd8}, data[2:2+2+13880]...)
	small = append(small, 0xff, 0xd9)
	ex, err = imagemeta.Decode(bytes.NewReader(small))
	t.Logf("APP1-only err=%v make=%q date=%v", err, ex.IFD0.Make, ex.SelectedDate())
}

// go-mp4 must expose Apple's creationdate key (it carries a UTC offset).
func TestProbeAppleKeys(t *testing.T) {
	f, _ := os.Open(corpus + "synth-apple.mov")
	defer f.Close()
	var keys []string
	var vals []string
	_, err := mp4.ReadBoxStructure(f, func(h *mp4.ReadHandle) (any, error) {
		switch {
		case h.BoxInfo.Type == mp4.BoxTypeMoov(), h.BoxInfo.Type == mp4.BoxTypeMeta(), h.BoxInfo.Type == mp4.BoxTypeIlst():
			return h.Expand()
		case h.BoxInfo.Type == mp4.BoxTypeKeys():
			b, _, err := h.ReadPayload()
			if err != nil {
				return nil, err
			}
			for _, e := range b.(*mp4.Keys).Entries {
				keys = append(keys, string(e.KeyValue))
			}
		case mp4.IsIlstMetaBoxType(h.BoxInfo.Type), h.BoxInfo.Context.UnderIlst:
			if h.BoxInfo.Type != mp4.BoxTypeData() {
				v, err := h.Expand()
				t.Logf("expand %q -> vals=%v err=%v", h.BoxInfo.Type.String(), v, err)
				return v, err
			}
			b, _, err := h.ReadPayload()
			if err != nil {
				return nil, err
			}
			vals = append(vals, string(b.(*mp4.Data).Data))
		case h.BoxInfo.Type == mp4.BoxTypeData():
			b, _, err := h.ReadPayload()
			if err != nil {
				return nil, err
			}
			vals = append(vals, string(b.(*mp4.Data).Data))
		}
		return nil, nil
	})
	t.Logf("err=%v keys=%q vals=%q", err, keys, vals)
}

// goexif builds times with time.Local; show it follows the host zone.
func TestGoexifZone(t *testing.T) {
	for _, tz := range []string{"UTC", "Asia/Tokyo"} {
		loc, _ := time.LoadLocation(tz)
		old := time.Local
		time.Local = loc
		f, _ := os.Open(corpus + "exiftool.Apple.jpg")
		x, _ := rwexif.Decode(f)
		f.Close()
		d, _ := x.DateTime()
		t.Logf("TZ=%s -> %s", tz, d.Format(time.RFC3339))
		time.Local = old
	}
}
