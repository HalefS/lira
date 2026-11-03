package photo

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func TestSniff(t *testing.T) {
	pngHead := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0, 0, 0, 13}

	tests := []struct {
		name string
		head []byte
		want string
	}{
		{"jpeg", []byte{0xFF, 0xD8, 0xFF, 0xE0}, "image/jpeg"},
		{"truncated jpeg", []byte{0xFF, 0xD8}, ""},
		{"png", pngHead, "image/png"},
		{"png magic only", []byte{0x89, 'P', 'N', 'G'}, ""},
		{"heic", []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'h', 'e', 'i', 'c'}, "image/heic"},
		{"mif1", []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'm', 'i', 'f', '1'}, "image/heic"},
		{"avif", []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'a', 'v', 'i', 'f'}, "image/heic"},
		{"html", []byte("<html><body>no</body></html>"), ""},
		{"svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`), ""},
		{"gif", []byte("GIF89a\x01\x00\x01\x00"), ""},
		{"webp", []byte("RIFF\x24\x00\x00\x00WEBPVP8 "), ""},
		{"empty", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Sniff(tt.head); got != tt.want {
				t.Fatalf("Sniff() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormaliseRejectsNonImages(t *testing.T) {
	tests := []struct {
		name string
		src  []byte
		want error
	}{
		{"empty", nil, ErrCorrupt},
		{"html", []byte("<html>hello</html>"), ErrUnsupported},
		{"gif", []byte("GIF89a\x01\x00\x01\x00"), ErrUnsupported},
		{"heic", []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'h', 'e', 'i', 'c', 0, 0, 0, 0}, ErrHEIC},
		{"jpeg header only", []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'x'}, ErrCorrupt},
		{"oversize input", make([]byte, MaxBytes+1), ErrTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Normalise(tt.src); !errors.Is(err, tt.want) {
				t.Fatalf("Normalise() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestNormaliseStripsEXIFAndAppliesOrientation(t *testing.T) {
	// 8 x 4, so an orientation of 6 has to produce 4 x 8. A square image would
	// pass even if the transform were never applied.
	base := encodeTestJPEG(t, 8, 4)
	src := withEXIF(base, 6, binary.BigEndian)

	if got := jpegOrientation(src); got != 6 {
		t.Fatalf("jpegOrientation() = %d, want 6", got)
	}

	res, err := Normalise(src)
	if err != nil {
		t.Fatalf("Normalise() error = %v", err)
	}
	if res.MimeType != "image/jpeg" {
		t.Fatalf("MimeType = %q, want image/jpeg", res.MimeType)
	}
	if res.Width != 4 || res.Height != 8 {
		t.Fatalf("dimensions = %dx%d, want 4x8", res.Width, res.Height)
	}
	if bytes.Contains(res.Data, []byte("Exif\x00\x00")) {
		t.Fatal("normalised JPEG still contains an EXIF header")
	}
	if hasAPP1(res.Data) {
		t.Fatal("normalised JPEG still contains an APP1 segment")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(res.Data))
	if err != nil || format != "jpeg" {
		t.Fatalf("re-decode = %q, %v", format, err)
	}
	if cfg.Width != 4 || cfg.Height != 8 {
		t.Fatalf("stored header dimensions = %dx%d, want 4x8", cfg.Width, cfg.Height)
	}
	if want := sha256.Sum256(res.Data); res.SHA256 != want {
		t.Fatal("SHA256 does not match the stored bytes")
	}
}

func TestJPEGOrientation(t *testing.T) {
	base := encodeTestJPEG(t, 4, 4)

	for o := 1; o <= 8; o++ {
		for _, bo := range []binary.ByteOrder{binary.BigEndian, binary.LittleEndian} {
			if got := jpegOrientation(withEXIF(base, o, bo)); got != o {
				t.Fatalf("orientation %d (%T) = %d", o, bo, got)
			}
		}
	}

	tests := []struct {
		name string
		src  []byte
	}{
		{"plain jpeg", base},
		{"empty", nil},
		{"not a jpeg", []byte("GIF89a")},
		{"truncated at SOI", []byte{0xFF, 0xD8}},
		{"truncated segment", []byte{0xFF, 0xD8, 0xFF, 0xE1, 0x00, 0x40, 'E'}},
		{"corrupt tiff offset", withRawEXIF(base, []byte("Exif\x00\x00MM\x00\x2A\x00\x00\x00\x02"))},
		{"short IFD", withRawEXIF(base, []byte("Exif\x00\x00MM\x00\x2A\x00\x00\x00\x08\xFF\xFF"))},
		{"orientation 0", withRawEXIF(base, tiffOrientation(binary.BigEndian, 0))},
		{"orientation 9", withRawEXIF(base, tiffOrientation(binary.BigEndian, 9))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := jpegOrientation(tt.src); got != 1 {
				t.Fatalf("jpegOrientation() = %d, want 1", got)
			}
		})
	}
}

func TestOrientPixels(t *testing.T) {
	// A 3 x 2 image whose pixels encode their own source coordinates.
	src := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 3; x++ {
			src.SetNRGBA(x, y, color.NRGBA{R: uint8(40*x + 1), G: uint8(40*y + 1), B: 7, A: 255})
		}
	}
	at := func(img image.Image, x, y int) color.NRGBA {
		return color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
	}
	srcAt := func(x, y int) color.NRGBA { return at(src, x, y) }

	tests := []struct {
		o      int
		w, h   int
		checks [][4]int // srcX, srcY, dstX, dstY
	}{
		{2, 3, 2, [][4]int{{0, 0, 2, 0}, {2, 0, 0, 0}, {1, 1, 1, 1}}},
		{3, 3, 2, [][4]int{{0, 0, 2, 1}, {2, 1, 0, 0}, {1, 0, 1, 1}}},
		{4, 3, 2, [][4]int{{0, 0, 0, 1}, {2, 1, 2, 0}}},
		{5, 2, 3, [][4]int{{0, 0, 0, 0}, {2, 1, 1, 2}, {1, 0, 0, 1}}},
		{6, 2, 3, [][4]int{{0, 0, 1, 0}, {2, 1, 0, 2}, {2, 0, 1, 2}}},
		{7, 2, 3, [][4]int{{0, 0, 1, 2}, {2, 0, 1, 0}, {1, 0, 1, 1}}},
		{8, 2, 3, [][4]int{{0, 0, 0, 2}, {2, 0, 0, 0}, {0, 1, 1, 2}}},
	}

	for _, tt := range tests {
		got := orient(src, tt.o)
		if b := got.Bounds(); b.Dx() != tt.w || b.Dy() != tt.h {
			t.Fatalf("orientation %d: bounds = %dx%d, want %dx%d", tt.o, b.Dx(), b.Dy(), tt.w, tt.h)
		}
		for _, c := range tt.checks {
			want := srcAt(c[0], c[1])
			if have := at(got, c[2], c[3]); have != want {
				t.Fatalf("orientation %d: pixel (%d,%d) = %v, want %v from (%d,%d)",
					tt.o, c[2], c[3], have, want, c[0], c[1])
			}
		}
	}

	// 1 means "already upright": the original is returned untouched.
	if got := orient(src, 1); got != image.Image(src) {
		t.Fatal("orientation 1 should return the original image")
	}
}

func TestNormaliseRejectsHugeDimensionsFromHeader(t *testing.T) {
	src := fakePNGHeader(t, 30000, 30000)
	if got := Sniff(src); got != "image/png" {
		t.Fatalf("Sniff() = %q, want image/png", got)
	}
	if _, err := Normalise(src); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("Normalise() error = %v, want ErrTooLarge", err)
	}
}

func TestNormaliseKeepsPNGAlpha(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			src.SetNRGBA(x, y, color.NRGBA{R: 200, G: 10, B: 10, A: 255})
		}
	}
	src.SetNRGBA(0, 0, color.NRGBA{R: 255, G: 0, B: 0, A: 0})
	src.SetNRGBA(1, 0, color.NRGBA{R: 0, G: 255, B: 0, A: 128})

	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}

	res, err := Normalise(buf.Bytes())
	if err != nil {
		t.Fatalf("Normalise() error = %v", err)
	}
	if res.MimeType != "image/png" {
		t.Fatalf("MimeType = %q, want image/png", res.MimeType)
	}
	img, err := png.Decode(bytes.NewReader(res.Data))
	if err != nil {
		t.Fatal(err)
	}
	if a := color.NRGBAModel.Convert(img.At(0, 0)).(color.NRGBA).A; a != 0 {
		t.Fatalf("transparent pixel alpha = %d, want 0", a)
	}
	if a := color.NRGBAModel.Convert(img.At(1, 0)).(color.NRGBA).A; a != 128 {
		t.Fatalf("semi-transparent pixel alpha = %d, want 128", a)
	}
}

func TestFlattenOnWhite(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	src.SetNRGBA(0, 0, color.NRGBA{R: 0, G: 0, B: 0, A: 0})

	got := flattenOnWhite(src)
	c := color.NRGBAModel.Convert(got.At(0, 0)).(color.NRGBA)
	if c.R != 255 || c.G != 255 || c.B != 255 || c.A != 255 {
		t.Fatalf("flattened pixel = %v, want opaque white", c)
	}
}

// --- helpers ---------------------------------------------------------------

func encodeTestJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(20 * x), G: uint8(40 * y), B: 90, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// withEXIF inserts an APP1 segment carrying just an orientation tag directly
// after the SOI, which is where cameras put it.
func withEXIF(jpg []byte, orientation int, bo binary.ByteOrder) []byte {
	return withRawEXIF(jpg, tiffOrientation(bo, orientation))
}

func withRawEXIF(jpg, payload []byte) []byte {
	// Two length bytes, then the payload. The length includes itself.
	segLen := len(payload) + 2
	app1 := []byte{0xFF, 0xE1, byte(segLen >> 8), byte(segLen)}
	app1 = append(app1, payload...)

	out := make([]byte, 0, len(jpg)+len(app1))
	out = append(out, jpg[:2]...) // SOI
	out = append(out, app1...)
	out = append(out, jpg[2:]...)
	return out
}

// tiffOrientation builds an APP1 payload: "Exif\0\0", a TIFF header, and a
// single-entry IFD0 holding tag 0x0112.
func tiffOrientation(bo binary.ByteOrder, orientation int) []byte {
	var b bytes.Buffer
	b.WriteString("Exif\x00\x00")
	if bo == binary.LittleEndian {
		b.WriteString("II")
	} else {
		b.WriteString("MM")
	}
	var tmp [4]byte
	bo.PutUint16(tmp[:2], 0x002A)
	b.Write(tmp[:2])
	bo.PutUint32(tmp[:4], 8) // IFD0 offset
	b.Write(tmp[:4])

	bo.PutUint16(tmp[:2], 1) // one entry
	b.Write(tmp[:2])
	bo.PutUint16(tmp[:2], 0x0112)
	b.Write(tmp[:2])
	bo.PutUint16(tmp[:2], 3) // SHORT
	b.Write(tmp[:2])
	bo.PutUint32(tmp[:4], 1) // count
	b.Write(tmp[:4])
	bo.PutUint16(tmp[:2], uint16(orientation))
	bo.PutUint16(tmp[2:4], 0)
	b.Write(tmp[:4])
	bo.PutUint32(tmp[:4], 0) // no next IFD
	b.Write(tmp[:4])
	return b.Bytes()
}

// hasAPP1 walks the JPEG segments looking for an APP1 marker.
func hasAPP1(jpg []byte) bool {
	if len(jpg) < 4 {
		return false
	}
	i := 2
	for i+4 <= len(jpg) && jpg[i] == 0xFF {
		marker := jpg[i+1]
		if marker == 0xDA || marker == 0xD9 {
			return false
		}
		n := int(jpg[i+2])<<8 | int(jpg[i+3])
		if n < 2 || i+2+n > len(jpg) {
			return false
		}
		if marker == 0xE1 {
			return true
		}
		i += 2 + n
	}
	return false
}

// fakePNGHeader builds a PNG whose IHDR claims the given size and nothing else.
// image.DecodeConfig reads the IHDR and stops, so the missing pixel data does
// not matter: the point is that the dimension check runs before any decode.
func fakePNGHeader(t *testing.T, w, h uint32) []byte {
	t.Helper()
	var b bytes.Buffer
	b.Write(pngMagic)

	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:4], w)
	binary.BigEndian.PutUint32(ihdr[4:8], h)
	ihdr[8] = 8 // bit depth
	ihdr[9] = 2 // truecolour
	// compression, filter, interlace all zero

	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(ihdr)))
	b.Write(length[:])
	b.WriteString("IHDR")
	b.Write(ihdr)

	crc := crc32.NewIEEE()
	crc.Write([]byte("IHDR"))
	crc.Write(ihdr)
	var sum [4]byte
	binary.BigEndian.PutUint32(sum[:], crc.Sum32())
	b.Write(sum[:])
	return b.Bytes()
}
