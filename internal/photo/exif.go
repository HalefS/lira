package photo

import (
	"bytes"
	"encoding/binary"
	"image"
)

// jpegOrientation returns the EXIF orientation of a JPEG, 1 when there is none
// or the metadata cannot be read. It never returns an error and never panics:
// malformed EXIF is common in the wild, and a photo that merely lacks a usable
// orientation tag is still a perfectly good photo.
//
// The parser is hand-written because encoding/binary can read the TIFF fields
// but nothing in the standard library exposes them. It walks the JPEG segment
// list looking for APP1, then IFD0 for tag 0x0112. That is the whole of EXIF
// this app needs; nothing else from the metadata is read, and all of it is
// discarded by the re-encode.
func jpegOrientation(src []byte) int {
	if len(src) < 4 || src[0] != 0xFF || src[1] != 0xD8 {
		return 1
	}

	i := 2
	for i+4 <= len(src) {
		if src[i] != 0xFF {
			return 1
		}
		marker := src[i+1]

		// Fill bytes: a segment may be preceded by any number of 0xFF.
		if marker == 0xFF {
			i++
			continue
		}
		// Markers with no length field.
		if marker == 0xD8 || marker == 0x01 || (marker >= 0xD0 && marker <= 0xD7) {
			i += 2
			continue
		}
		// Start of scan or end of image: no metadata follows.
		if marker == 0xDA || marker == 0xD9 {
			return 1
		}
		if i+4 > len(src) {
			return 1
		}
		segLen := int(src[i+2])<<8 | int(src[i+3])
		if segLen < 2 || i+2+segLen > len(src) {
			return 1
		}
		if marker == 0xE1 {
			if o := exifOrientation(src[i+4 : i+2+segLen]); o != 0 {
				return o
			}
		}
		i += 2 + segLen
	}
	return 1
}

// exifOrientation reads tag 0x0112 out of an APP1 payload. Returns 0 when the
// payload is not EXIF, has no orientation tag, or is malformed.
func exifOrientation(b []byte) int {
	if len(b) < 14 || !bytes.HasPrefix(b, []byte("Exif\x00\x00")) {
		return 0
	}
	t := b[6:]

	var bo binary.ByteOrder
	switch {
	case t[0] == 'I' && t[1] == 'I':
		bo = binary.LittleEndian
	case t[0] == 'M' && t[1] == 'M':
		bo = binary.BigEndian
	default:
		return 0
	}

	if len(t) < 8 || bo.Uint16(t[2:4]) != 0x002A {
		return 0
	}
	off := int(bo.Uint32(t[4:8]))
	if off < 8 || off+2 > len(t) {
		return 0
	}

	n := int(bo.Uint16(t[off : off+2]))
	if n < 1 || n > 512 {
		return 0
	}
	p := off + 2
	for k := 0; k < n; k++ {
		if p+12 > len(t) {
			return 0
		}
		tag := bo.Uint16(t[p : p+2])
		typ := bo.Uint16(t[p+2 : p+4])
		count := bo.Uint32(t[p+4 : p+8])
		if tag == 0x0112 {
			if typ != 3 || count != 1 {
				return 0
			}
			// A SHORT lives in the low two bytes of the four-byte value field,
			// which is the first two bytes for both byte orders.
			v := int(bo.Uint16(t[p+8 : p+10]))
			if v >= 1 && v <= 8 {
				return v
			}
			return 0
		}
		p += 12
	}
	return 0
}

// orient applies an EXIF orientation to an image and returns it with its bounds
// starting at (0, 0). Orientation 5-8 swap width and height.
func orient(img image.Image, o int) image.Image {
	if o < 2 || o > 8 {
		return img
	}

	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	dw, dh := w, h
	if o >= 5 {
		dw, dh = h, w
	}
	dst := image.NewNRGBA(image.Rect(0, 0, dw, dh))

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var dx, dy int
			switch o {
			case 2: // mirror horizontal
				dx, dy = w-1-x, y
			case 3: // rotate 180
				dx, dy = w-1-x, h-1-y
			case 4: // mirror vertical
				dx, dy = x, h-1-y
			case 5: // transpose
				dx, dy = y, x
			case 6: // rotate 90 clockwise
				dx, dy = h-1-y, x
			case 7: // transverse
				dx, dy = h-1-y, w-1-x
			case 8: // rotate 270 clockwise
				dx, dy = y, w-1-x
			}
			dst.Set(dx, dy, img.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}
