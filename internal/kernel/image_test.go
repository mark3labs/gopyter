package kernel

import (
	"bytes"
	"context"
	"encoding/base64"
	"image/png"
	"testing"
)

func TestDisplayImage(t *testing.T) {
	k, err := New("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := k.Close(); err != nil {
			t.Error(err)
		}
	})

	// A trailing image expression, Display mixing text and images, and a
	// nil image, which must not panic.
	src := `img := image.NewRGBA(image.Rect(0, 0, 3, 2))
img.Set(1, 1, color.RGBA{255, 0, 0, 255})
Display("before", img, "after")
var none *image.RGBA
Display(none)
img`
	var events []Event
	err = k.Execute(context.Background(), "1", "In[1]", src, func(e Event) { events = append(events, e) })
	if err != nil {
		t.Fatalf("execute: %v %+v", err, events)
	}
	var kinds []EventKind
	for _, e := range events {
		kinds = append(kinds, e.Kind)
	}
	want := []EventKind{Result, Image, Result, Result, Image}
	if len(kinds) != len(want) {
		t.Fatalf("events %v, want kinds %v", events, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("event %d is %v, want %v (%+v)", i, kinds[i], want[i], events)
		}
	}
	if events[0].Text != "before" || events[2].Text != "after" || events[3].Text != "<nil>" {
		t.Fatalf("text results: %+v", events)
	}
	data, err := base64.StdEncoding.DecodeString(events[4].Text)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 3 || b.Dy() != 2 {
		t.Fatalf("image size %v", b)
	}
	if r, _, _, _ := img.At(1, 1).RGBA(); r != 0xffff {
		t.Fatalf("pixel not red: %v", img.At(1, 1))
	}
}
