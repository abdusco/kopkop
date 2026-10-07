package imageproc

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/abdusco/kopkop/internal/filesystem"
	"github.com/stretchr/testify/require"
)

func resizeURL(p *Processor, path string, width, height int) (string, error) {
	res, err := p.Process(path, OpScale, width, height)
	return res.URL, err
}

func TestResizeUniqueNamesAndConcurrentCache(t *testing.T) {
	source, output := filesystem.NewMemoryFS(), filesystem.NewMemoryFS()
	for _, tc := range []struct {
		path  string
		color color.RGBA
	}{
		{"a/photo.png", color.RGBA{R: 255, A: 255}},
		{"b/photo.png", color.RGBA{B: 255, A: 255}},
		{"copy/photo.png", color.RGBA{R: 255, A: 255}},
	} {
		var encoded bytes.Buffer
		img := image.NewRGBA(image.Rect(0, 0, 2, 2))
		for y := range 2 {
			for x := range 2 {
				img.Set(x, y, tc.color)
			}
		}
		require.NoError(t, png.Encode(&encoded, img))
		require.NoError(t, source.WriteFile(tc.path, encoded.Bytes(), 0o644))
	}
	var calls atomic.Int32
	p := New(source, output)
	p.backends = []resizeBackend{{name: "go", run: func(params ResizeParams) ([]byte, error) {
		calls.Add(1)
		return resizeWithGo(params)
	}}}
	urls := make([]string, 30)
	errors := make([]error, len(urls))
	var wg sync.WaitGroup
	for i := range urls {
		wg.Add(1)
		go func() {
			defer wg.Done()
			urls[i], errors[i] = resizeURL(p, "a/photo.png", 2, 2)
		}()
	}
	wg.Wait()
	for i := range urls {
		require.NoError(t, errors[i])
		require.Equal(t, urls[0], urls[i])
	}
	require.Equal(t, int32(1), calls.Load())
	copyURL, err := resizeURL(p, "copy/photo.png", 2, 2)
	require.NoError(t, err)
	require.Equal(t, urls[0], copyURL)
	otherURL, err := resizeURL(p, "b/photo.png", 2, 2)
	require.NoError(t, err)
	require.NotEqual(t, urls[0], otherURL)
	red, err := output.ReadFile(urls[0][1:])
	require.NoError(t, err)
	blue, err := output.ReadFile(otherURL[1:])
	require.NoError(t, err)
	require.NotEqual(t, red, blue)
	sizeURL, err := resizeURL(p, "a/photo.png", 3, 2)
	require.NoError(t, err)
	require.NotEqual(t, urls[0], sizeURL)
	changed, err := source.ReadFile("b/photo.png")
	require.NoError(t, err)
	require.NoError(t, source.WriteFile("a/photo.png", changed, 0o644))
	changedURL, err := resizeURL(p, "a/photo.png", 2, 2)
	require.NoError(t, err)
	require.Equal(t, otherURL, changedURL)
	require.Equal(t, int32(3), calls.Load())
}

func TestResizeRetriesFailedTransforms(t *testing.T) {
	source := filesystem.NewMemoryFS()
	var encoded bytes.Buffer
	require.NoError(t, png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	require.NoError(t, source.WriteFile("photo.png", encoded.Bytes(), 0o644))
	p := New(source, filesystem.NewMemoryFS())
	calls := 0
	p.backends = []resizeBackend{{name: "test", run: func(params ResizeParams) ([]byte, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("transient failure")
		}
		return resizeWithGo(params)
	}}}
	_, err := resizeURL(p, "photo.png", 1, 1)
	require.ErrorContains(t, err, "transient failure")
	_, err = resizeURL(p, "photo.png", 1, 1)
	require.NoError(t, err)
	require.Equal(t, 2, calls)
}
