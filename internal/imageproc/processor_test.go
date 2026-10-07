package imageproc

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/abdusco/kopkop/internal/filesystem"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetadataAndResize(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "images"), 0o755))

	imgPath := filepath.Join(root, "images", "sample.png")
	img := image.NewRGBA(image.Rect(0, 0, 20, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 20; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 0, A: 255})
		}
	}
	f, err := os.Create(imgPath)
	require.NoError(t, err)
	require.NoError(t, png.Encode(f, img))
	require.NoError(t, f.Close())

	p := New(filesystem.NewDiskFS(root), filesystem.NewDiskFS(root))
	md, err := p.GetMetadata("images/sample.png")
	require.NoError(t, err)
	assert.Equal(t, 20, md.Width)
	assert.Equal(t, 10, md.Height)

	res, err := p.Process("images/sample.png", OpScale, 8, 4)
	require.NoError(t, err)
	assert.Contains(t, res.URL, "/processed_images/")
	require.FileExists(t, filepath.Join(root, filepath.FromSlash(res.URL[1:])))
}

func TestResizeWithBackends_FallsThroughOnError(t *testing.T) {
	called := make([]string, 0, 3)
	input := []byte("input")
	backends := []resizeBackend{
		{
			name: "vips",
			run: func(params ResizeParams) ([]byte, error) {
				called = append(called, "vips")
				return nil, errors.New("vips failed")
			},
		},
		{
			name: "magick",
			run: func(params ResizeParams) ([]byte, error) {
				called = append(called, "magick")
				return nil, errors.New("magick failed")
			},
		},
		{
			name: "go",
			run: func(params ResizeParams) ([]byte, error) {
				called = append(called, "go")
				return []byte("ok"), nil
			},
		},
	}

	out, err := resizeWithBackends(backends, ResizeParams{Input: input, Ext: ".png", Width: 10, Height: 5})
	require.NoError(t, err)
	assert.Equal(t, []byte("ok"), out)
	assert.Equal(t, []string{"vips", "magick", "go"}, called)
}

func TestResizeWithBackends_ReturnsCombinedErrorWhenAllFail(t *testing.T) {
	backends := []resizeBackend{
		{
			name: "vips",
			run: func(params ResizeParams) ([]byte, error) {
				return nil, errors.New("boom1")
			},
		},
		{
			name: "magick",
			run: func(params ResizeParams) ([]byte, error) {
				return nil, errors.New("boom2")
			},
		},
	}

	_, err := resizeWithBackends(backends, ResizeParams{Input: []byte("input"), Ext: ".png", Width: 10, Height: 5})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "all resize backends failed")
	assert.Contains(t, err.Error(), "vips: boom1")
	assert.Contains(t, err.Error(), "magick: boom2")
}

func TestProcess_OperationsKeepAspectRatio(t *testing.T) {
	t.Parallel()

	// 40x20 source: left half red, right half blue.
	img := image.NewRGBA(image.Rect(0, 0, 40, 20))
	for y := 0; y < 20; y++ {
		for x := 0; x < 40; x++ {
			c := color.RGBA{R: 255, A: 255}
			if x >= 20 {
				c = color.RGBA{B: 255, A: 255}
			}
			img.Set(x, y, c)
		}
	}
	var pngBuf bytes.Buffer
	require.NoError(t, png.Encode(&pngBuf, img))

	for _, tc := range []struct {
		name          string
		op            string
		width, height int
		wantW, wantH  int
		wantErr       string
	}{
		{"scale stretches", OpScale, 10, 10, 10, 10, ""},
		{"fit_width", OpFitWidth, 10, 0, 10, 5, ""},
		{"fit_height", OpFitHeight, 0, 5, 10, 5, ""},
		{"fit by width", OpFit, 10, 10, 10, 5, ""},
		{"fit by height", OpFit, 100, 5, 10, 5, ""},
		{"fill", OpFill, 10, 10, 10, 10, ""},
		{"fit_width needs width", OpFitWidth, 0, 5, 0, 0, "positive width"},
		{"scale needs both", OpScale, 10, 0, 0, 0, "positive height"},
		{"unknown op", "zoom", 10, 10, 0, 0, "unknown resize operation"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mfs := filesystem.NewMemoryFS()
			require.NoError(t, mfs.WriteFile("a.png", pngBuf.Bytes(), 0o644))
			p := New(mfs, mfs)
			p.backends = []resizeBackend{{name: "go", run: resizeWithGo}}

			res, err := p.Process("a.png", tc.op, tc.width, tc.height)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantW, res.Width)
			assert.Equal(t, tc.wantH, res.Height)
			out, err := mfs.ReadFile(res.StaticPath)
			require.NoError(t, err)
			conf, _, err := image.DecodeConfig(bytes.NewReader(out))
			require.NoError(t, err)
			assert.Equal(t, tc.wantW, conf.Width)
			assert.Equal(t, tc.wantH, conf.Height)
		})
	}
}

func TestCentreCrop(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		src  image.Rectangle
		w, h int
		want image.Rectangle
	}{
		{"wide source to square", image.Rect(0, 0, 40, 20), 10, 10, image.Rect(10, 0, 30, 20)},
		{"tall source to square", image.Rect(0, 0, 20, 40), 10, 10, image.Rect(0, 10, 20, 30)},
		{"same ratio", image.Rect(0, 0, 40, 20), 20, 10, image.Rect(0, 0, 40, 20)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, centreCrop(tc.src, tc.w, tc.h))
		})
	}
}

func TestResize_WritesViaProvidedFS(t *testing.T) {
	t.Parallel()

	img := image.NewRGBA(image.Rect(0, 0, 6, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 6; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 100, B: 50, A: 255})
		}
	}
	var pngBuf bytes.Buffer
	require.NoError(t, png.Encode(&pngBuf, img))

	mfs := filesystem.NewMemoryFS()
	require.NoError(t, mfs.WriteFile("images/sample.png", pngBuf.Bytes(), 0o644))
	p := New(mfs, mfs)

	res, err := p.Process("images/sample.png", OpScale, 3, 2)
	require.NoError(t, err)
	assert.Regexp(t, `^/processed_images/[0-9a-f]{64}-3x2\.png$`, res.URL)

	out, err := mfs.ReadFile(res.StaticPath)
	require.NoError(t, err)
	conf, _, err := image.DecodeConfig(bytes.NewReader(out))
	require.NoError(t, err)
	assert.Equal(t, 3, conf.Width)
	assert.Equal(t, 2, conf.Height)
}
