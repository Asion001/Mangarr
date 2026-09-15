package reading

import (
	"image"
	"image/color"
	"testing"
)

func TestContentBox(t *testing.T) {
	// a 400x600 page: white margins of 40 (sides) and 60 (top/bottom) around dark content
	img := image.NewRGBA(image.Rect(0, 0, 400, 600))
	for y := 0; y < 600; y++ {
		for x := 0; x < 400; x++ {
			c := color.RGBA{255, 255, 255, 255}
			if x >= 40 && x < 360 && y >= 60 && y < 540 {
				c = color.RGBA{uint8(x % 200), uint8(y % 200), 90, 255}
			}
			img.Set(x, y, c)
		}
	}
	b := ContentBox(img)
	if b.Width != 400 || b.Height != 600 {
		t.Fatalf("size %+v", b)
	}
	// content 40..360 x 60..540, plus a 1% margin
	if b.X != 36 || b.Y != 54 || b.W != 328 || b.H != 492 {
		t.Fatalf("box %+v", b)
	}
	// a blank page isn't cropped
	blank := image.NewRGBA(image.Rect(0, 0, 400, 600))
	for i := range blank.Pix {
		blank.Pix[i] = 255
	}
	if b := ContentBox(blank); b.X != 0 || b.W != 400 || b.H != 600 {
		t.Fatalf("blank %+v", b)
	}
	// black borders too
	for y := 0; y < 600; y++ {
		for x := 0; x < 400; x++ {
			if x < 40 || x >= 360 {
				img.Set(x, y, color.Black)
			}
		}
	}
	if b := ContentBox(img); b.X < 30 || b.W > 340 {
		t.Fatalf("black sides %+v", b)
	}
}
