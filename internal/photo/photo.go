// Package photo turns an uploaded image into the bytes LIRA stores.
//
// It exists because a phone photo is not safe to store as uploaded. The bytes
// on the camera roll carry EXIF, which includes the GPS coordinates of the room
// the technician was standing in, and the bytes may not be an image at all. So
// every upload is sniffed, bounded, decoded, rotated upright, and re-encoded.
// The re-encode is what removes the metadata; there is no separate stripping
// pass that could be skipped.
//
// Nothing in this package touches HTTP or the database. It takes bytes and
// returns bytes, which is what makes the awkward parts -- orientation, alpha,
// the size and dimension caps -- testable without a request or a Postgres.
package photo

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
)

// Limits. The client is expected to shrink a photo to a 1600 px edge before it
// is ever sent, so these are backstops for a client that does not, not targets.
const (
	// MaxBytes is the largest stored image, and therefore the largest upload.
	MaxBytes = 8 << 20

	// MaxPixels bounds the memory a decode can allocate. 20 MP is a large phone
	// sensor; a 30000 x 30000 PNG would be 3.6 GB as RGBA and is rejected from
	// its header before any pixels are read.
	MaxPixels = 20_000_000

	// MaxEdge keeps a panorama or a stitched screenshot from being accepted on
	// pixel count alone while being unusable in a 68 px tile.
	MaxEdge = 10_000

	// JPEGQuality is high enough to read an equipment label and low enough that
	// a 1600 px photo lands around 300 KB.
	JPEGQuality = 82

	// retryQuality is used once when the first encode exceeds MaxBytes -- which
	// can only happen for a very large, very detailed upload.
	retryQuality = 70

	// maxPNGPixels is the point past which a PNG is re-encoded as JPEG instead.
	// PNG is kept for screenshots and diagrams, where lossless text matters;
	// a 20 MP photographic PNG is both pointless and enormous.
	maxPNGPixels = 4_000_000
)

var (
	// ErrUnsupported: the bytes are not a JPEG or a PNG. This includes WebP,
	// GIF, SVG and HTML, all of which the standard library either cannot decode
	// or should not serve back from the app's own origin.
	ErrUnsupported = errors.New("photo: unsupported image format")

	// ErrHEIC: an iPhone photo that never went through the client's canvas
	// conversion. Kept separate from ErrUnsupported so the UI can say something
	// useful ("set Camera to Most Compatible") instead of "not an image".
	ErrHEIC = errors.New("photo: HEIC is not supported")

	// ErrTooLarge: the encoded upload, or the decoded image, is over a limit.
	ErrTooLarge = errors.New("photo: image is too large")

	// ErrCorrupt: the bytes claimed to be a JPEG or a PNG but would not decode.
	ErrCorrupt = errors.New("photo: image could not be decoded")
)

// Result is what the database row and the HTTP response are built from. Data is
// the normalised bytes, never the upload.
type Result struct {
	Data     []byte
	MimeType string
	Width    int
	Height   int
	SHA256   [32]byte
}

var pngMagic = []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}

// HEIF brands. "mif1" and "msf1" are the generic HEIF brands iPhone and some
// Android cameras write when the still is HEVC; "avif" is AVIF, equally
// undecodable here and equally worth the same message to the user.
var heifBrands = map[string]bool{
	"heic": true,
	"heix": true,
	"hevc": true,
	"hevx": true,
	"heim": true,
	"heis": true,
	"mif1": true,
	"msf1": true,
	"avif": true,
}

// Sniff reports what the first bytes are, without decoding them:
// "image/jpeg", "image/png", "image/heic", or "" for anything else.
//
// This is deliberately stricter than http.DetectContentType. The standard
// library sniffer knows WebP and GIF, and both would be accepted by a check
// built on it while this package cannot decode them.
func Sniff(head []byte) string {
	switch {
	case len(head) >= 3 && head[0] == 0xFF && head[1] == 0xD8 && head[2] == 0xFF:
		return "image/jpeg"
	case len(head) >= len(pngMagic) && bytes.Equal(head[:len(pngMagic)], pngMagic):
		return "image/png"
	case len(head) >= 12 && string(head[4:8]) == "ftyp" && heifBrands[string(head[8:12])]:
		return "image/heic"
	}
	return ""
}

// Normalise is the only entry point used by the HTTP handler. It never trusts
// anything but the bytes: it sniffs, bounds, decodes, applies EXIF orientation,
// re-encodes (which is what strips the metadata), measures and hashes.
func Normalise(src []byte) (Result, error) {
	if len(src) == 0 {
		return Result{}, ErrCorrupt
	}
	if len(src) > MaxBytes {
		return Result{}, ErrTooLarge
	}

	switch Sniff(src) {
	case "image/heic":
		return Result{}, ErrHEIC
	case "image/jpeg", "image/png":
	default:
		return Result{}, ErrUnsupported
	}

	// Bounds first, from the header only. A 30000 x 30000 PNG must be refused
	// before image.Decode allocates anything.
	cfg, format, err := image.DecodeConfig(bytes.NewReader(src))
	if err != nil {
		return Result{}, ErrCorrupt
	}
	if (format != "jpeg" && format != "png") || cfg.Width < 1 || cfg.Height < 1 {
		return Result{}, ErrUnsupported
	}
	pixels := int64(cfg.Width) * int64(cfg.Height)
	if pixels > MaxPixels || cfg.Width > MaxEdge || cfg.Height > MaxEdge {
		return Result{}, ErrTooLarge
	}

	img, _, err := image.Decode(bytes.NewReader(src))
	if err != nil {
		return Result{}, ErrCorrupt
	}

	// Go's JPEG decoder does not read EXIF orientation, and re-encoding drops
	// the tag. Without this step every portrait photo would be stored sideways
	// with nothing left to say so. PNG eXIf is rare and is dropped rather than
	// honoured; the client's canvas bakes orientation in for both.
	if format == "jpeg" {
		img = orient(img, jpegOrientation(src))
	}

	data, mimeType, err := encode(img, format, pixels)
	if err != nil {
		return Result{}, err
	}

	b := img.Bounds()
	if b.Dx() < 1 || b.Dy() < 1 {
		return Result{}, ErrCorrupt
	}
	return Result{
		Data:     data,
		MimeType: mimeType,
		Width:    b.Dx(),
		Height:   b.Dy(),
		SHA256:   sha256.Sum256(data),
	}, nil
}

// encode chooses the stored format. PNG in stays PNG while it is a screenshot-
// sized image, because re-encoding a screenshot as JPEG makes its text mush;
// everything else becomes JPEG on white.
func encode(img image.Image, format string, pixels int64) ([]byte, string, error) {
	if format == "png" && pixels <= maxPNGPixels {
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err == nil && buf.Len() <= MaxBytes {
			return buf.Bytes(), "image/png", nil
		}
	}

	data, err := encodeJPEG(img, JPEGQuality)
	if err != nil {
		return nil, "", ErrCorrupt
	}
	if len(data) > MaxBytes {
		data, err = encodeJPEG(img, retryQuality)
		if err != nil {
			return nil, "", ErrCorrupt
		}
	}
	if len(data) > MaxBytes {
		return nil, "", ErrTooLarge
	}
	return data, "image/jpeg", nil
}

func encodeJPEG(img image.Image, quality int) ([]byte, error) {
	if needsFlatten(img) {
		img = flattenOnWhite(img)
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type opaqueImage interface{ Opaque() bool }

// needsFlatten reports whether an image has transparency. jpeg.Encode ignores
// alpha, which encodes a transparent pixel as black; the app wants white, the
// colour of paper and of the wall behind a diagram.
func needsFlatten(img image.Image) bool {
	if o, ok := img.(opaqueImage); ok {
		return !o.Opaque()
	}
	return true
}

// flattenOnWhite composites img onto an opaque white canvas, preserving size.
func flattenOnWhite(img image.Image) image.Image {
	b := img.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(dst, dst.Bounds(), img, b.Min, draw.Over)
	return dst
}
