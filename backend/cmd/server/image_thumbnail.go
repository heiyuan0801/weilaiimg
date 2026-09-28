package main

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
)

// generateImageThumbnail creates a bounded JPEG preview while keeping the
// original object untouched. The standard library decoders cover PNG, JPEG
// and GIF; formats without a built-in decoder continue to use the original
// media URL until an optional image worker is configured.
func generateImageThumbnail(data []byte) ([]byte, error) {
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	bounds := src.Bounds()
	if bounds.Dx() == 0 || bounds.Dy() == 0 {
		return nil, fmt.Errorf("image has no pixels")
	}
	const maxEdge = 360
	width, height := bounds.Dx(), bounds.Dy()
	if width > maxEdge || height > maxEdge {
		if width >= height {
			height = height * maxEdge / width
			width = maxEdge
		} else {
			width = width * maxEdge / height
			height = maxEdge
		}
	}
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		sy := bounds.Min.Y + y*bounds.Dy()/height
		for x := 0; x < width; x++ {
			sx := bounds.Min.X + x*bounds.Dx()/width
			dst.Set(x, y, src.At(sx, sy))
		}
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 82}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
