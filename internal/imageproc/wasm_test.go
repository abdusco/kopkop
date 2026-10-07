package imageproc

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResizeWithWasm(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 200, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 200; x++ {
			source.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y * 2), B: 128, A: 255})
		}
	}
	var pngBytes, jpgBytes bytes.Buffer
	require.NoError(t, png.Encode(&pngBytes, source))
	require.NoError(t, jpeg.Encode(&jpgBytes, source, nil))

	for _, tc := range []struct {
		name    string
		input   []byte
		ext     string
		w, h    int
		crop    bool
		wantErr string
	}{
		{name: "png same aspect", input: pngBytes.Bytes(), ext: ".png", w: 100, h: 50},
		{name: "jpeg same aspect", input: jpgBytes.Bytes(), ext: ".jpg", w: 40, h: 20},
		{name: "png crop to square", input: pngBytes.Bytes(), ext: ".png", w: 50, h: 50, crop: true},
		{name: "jpeg crop to tall", input: jpgBytes.Bytes(), ext: ".jpg", w: 30, h: 60, crop: true},
		{name: "stretch", input: pngBytes.Bytes(), ext: ".png", w: 50, h: 50},
		{name: "upscale", input: pngBytes.Bytes(), ext: ".png", w: 400, h: 200},
		{name: "upscale crop", input: pngBytes.Bytes(), ext: ".png", w: 400, h: 400, crop: true},
		{name: "gif is unsupported", input: pngBytes.Bytes(), ext: ".gif", w: 100, h: 50, wantErr: "does not support"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := resizeWithWasm(ResizeParams{Input: tc.input, Ext: tc.ext, Width: tc.w, Height: tc.h, Crop: tc.crop})
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			conf, _, err := image.DecodeConfig(bytes.NewReader(out))
			require.NoError(t, err)
			require.Equal(t, tc.w, conf.Width)
			require.Equal(t, tc.h, conf.Height)
		})
	}
}
