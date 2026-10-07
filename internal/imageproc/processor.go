package imageproc

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/abdusco/kopkop/internal/filesystem"
	"golang.org/x/image/draw"
)

type Metadata struct {
	Width  int
	Height int
	Format string
}

type Processor struct {
	SourceFS filesystem.FileSystem
	OutputFS filesystem.FileSystem
	mu       sync.Mutex
	resizes  map[string]*resizeResult
	backends []resizeBackend
}

type resizeResult struct {
	done   chan struct{}
	result Result
	err    error
}

// ResizeParams is the final output size; Crop first centre-crops the source to
// that aspect ratio instead of stretching it.
type ResizeParams struct {
	Input  []byte
	Ext    string
	Width  int
	Height int
	Crop   bool
}

type resizeBackend struct {
	name string
	run  func(params ResizeParams) ([]byte, error)
}

var availableBackends = func() []resizeBackend {
	backends := make([]resizeBackend, 0, 3)
	if _, err := exec.LookPath("vips"); err == nil {
		backends = append(backends, resizeBackend{name: "vips", run: resizeWithVips})
	}
	if _, err := exec.LookPath("magick"); err == nil {
		backends = append(backends, resizeBackend{name: "magick", run: resizeWithMagick})
	}
	backends = append(backends,
		resizeBackend{name: "wasm", run: resizeWithWasm},
		resizeBackend{name: "go", run: resizeWithGo},
	)
	return backends
}()

func New(sourceFS filesystem.FileSystem, outputFS filesystem.FileSystem) *Processor {
	return &Processor{SourceFS: sourceFS, OutputFS: outputFS, resizes: map[string]*resizeResult{}, backends: availableBackends}
}

func (p *Processor) GetMetadata(relPath string) (Metadata, error) {
	f, err := p.SourceFS.Open(filepath.ToSlash(relPath))
	if err != nil {
		return Metadata{}, err
	}
	defer f.Close()
	return p.GetMetadataReader(f)
}

func (p *Processor) GetMetadataReader(r io.Reader) (Metadata, error) {
	conf, format, err := image.DecodeConfig(r)
	if err != nil {
		return Metadata{}, err
	}
	return Metadata{Width: conf.Width, Height: conf.Height, Format: format}, nil
}

// Resize operations, matching Zola's resize_image.
const (
	OpScale     = "scale"      // exactly width x height, ignoring the aspect ratio
	OpFitWidth  = "fit_width"  // resize to width, height follows the aspect ratio
	OpFitHeight = "fit_height" // resize to height, width follows the aspect ratio
	OpFit       = "fit"        // largest size inside width x height, keeping the aspect ratio
	OpFill      = "fill"       // cover width x height, then crop the centre
)

// Result describes a processed image. URL is root-relative ("/processed_images/...").
type Result struct {
	URL        string
	StaticPath string
	Width      int
	Height     int
}

// Process resizes the image at relPath. Width or height may be 0 when the
// operation derives it (fit_width, fit_height).
func (p *Processor) Process(relPath string, op string, width int, height int) (Result, error) {
	srcBytes, err := p.SourceFS.ReadFile(filepath.ToSlash(relPath))
	if err != nil {
		return Result{}, err
	}

	ext := extensionFromImageData(srcBytes)
	if ext == "" {
		return Result{}, fmt.Errorf("unable to detect image format for %q", relPath)
	}
	conf, _, err := image.DecodeConfig(bytes.NewReader(srcBytes))
	if err != nil {
		return Result{}, fmt.Errorf("read image %q: %w", relPath, err)
	}
	width, height, crop, err := targetSize(op, width, height, conf.Width, conf.Height)
	if err != nil {
		return Result{}, err
	}

	// Include the transformation version, operation, format, dimensions,
	// backend policy, and source bytes. Identical transforms share a name
	// regardless of path.
	digest := sha256.New()
	fmt.Fprintf(digest, "kopkop-resize-v2:%t:%d:%d:%s:", crop, width, height, ext)
	for _, backend := range p.backends {
		fmt.Fprintf(digest, "%s:", backend.name)
	}
	digest.Write(srcBytes)
	outRel := fmt.Sprintf("processed_images/%x-%dx%d%s", digest.Sum(nil), width, height, ext)
	p.mu.Lock()
	if result := p.resizes[outRel]; result != nil {
		p.mu.Unlock()
		<-result.done
		return result.result, result.err
	}
	result := &resizeResult{done: make(chan struct{})}
	p.resizes[outRel] = result
	p.mu.Unlock()
	result.err = p.resizeTo(outRel, ResizeParams{Input: srcBytes, Ext: ext, Width: width, Height: height, Crop: crop})
	result.result = Result{URL: "/" + outRel, StaticPath: outRel, Width: width, Height: height}
	p.mu.Lock()
	if result.err != nil {
		delete(p.resizes, outRel) // Failed writes and transforms may be retried.
	}
	close(result.done)
	p.mu.Unlock()
	return result.result, result.err
}

// targetSize returns the output size for op and whether the source must be
// centre-cropped to the target aspect ratio.
func targetSize(op string, width, height, srcW, srcH int) (w, h int, crop bool, err error) {
	if width < 0 || height < 0 {
		return 0, 0, false, fmt.Errorf("resize dimensions must not be negative")
	}
	need := func(name string, v int) error {
		if v <= 0 {
			return fmt.Errorf("resize operation %q needs a positive %s", op, name)
		}
		return nil
	}
	scaled := func(v, num, den int) int { return max(1, (v*num+den/2)/den) }
	switch op {
	case OpScale:
		if err := errors.Join(need("width", width), need("height", height)); err != nil {
			return 0, 0, false, err
		}
		return width, height, false, nil
	case OpFitWidth:
		if err := need("width", width); err != nil {
			return 0, 0, false, err
		}
		return width, scaled(srcH, width, srcW), false, nil
	case OpFitHeight:
		if err := need("height", height); err != nil {
			return 0, 0, false, err
		}
		return scaled(srcW, height, srcH), height, false, nil
	case OpFit:
		if err := errors.Join(need("width", width), need("height", height)); err != nil {
			return 0, 0, false, err
		}
		if width*srcH <= height*srcW { // width is the limiting side
			return width, scaled(srcH, width, srcW), false, nil
		}
		return scaled(srcW, height, srcH), height, false, nil
	case OpFill:
		if err := errors.Join(need("width", width), need("height", height)); err != nil {
			return 0, 0, false, err
		}
		return width, height, true, nil
	}
	return 0, 0, false, fmt.Errorf("unknown resize operation %q (want scale, fit_width, fit_height, fit or fill)", op)
}

func (p *Processor) resizeTo(outRel string, params ResizeParams) error {
	outBytes, err := resizeWithBackends(p.backends, params)
	if err != nil {
		return err
	}

	if err := p.OutputFS.MkdirAll(filepath.ToSlash(filepath.Dir(outRel)), 0o755); err != nil {
		return err
	}
	return p.OutputFS.WriteFile(outRel, outBytes, 0o644)
}

func extensionFromImageData(data []byte) string {
	if len(data) == 0 {
		return ""
	}

	head := data
	if len(head) > 512 {
		head = head[:512]
	}
	if sniffed := http.DetectContentType(head); sniffed != "" {
		switch sniffed {
		case "image/jpeg":
			return ".jpg"
		case "image/png":
			return ".png"
		case "image/webp":
			return ".webp"
		case "image/gif":
			return ".gif"
		}
	}

	if _, format, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
		switch strings.ToLower(format) {
		case "jpeg", "jpg":
			return ".jpg"
		case "png":
			return ".png"
		case "webp":
			return ".webp"
		case "gif":
			return ".gif"
		}
	}

	return ""
}

func resizeWithBackends(backends []resizeBackend, params ResizeParams) ([]byte, error) {
	errMsgs := make([]string, 0, len(backends))
	for _, backend := range backends {
		out, err := backend.run(params)
		if err != nil {
			errMsgs = append(errMsgs, fmt.Sprintf("%s: %v", backend.name, err))
			continue
		}
		return out, nil
	}

	return nil, fmt.Errorf("all resize backends failed: %s", strings.Join(errMsgs, "; "))
}

func resizeWithVips(params ResizeParams) ([]byte, error) {
	src, err := os.CreateTemp("", "kopkop-vips-src-*"+params.Ext)
	if err != nil {
		return nil, err
	}
	srcPath := src.Name()
	defer os.Remove(srcPath)
	if _, err := src.Write(params.Input); err != nil {
		src.Close()
		return nil, err
	}
	if err := src.Close(); err != nil {
		return nil, err
	}

	out, err := os.CreateTemp("", "kopkop-vips-out-*"+params.Ext)
	if err != nil {
		return nil, err
	}
	outPath := out.Name()
	if err := out.Close(); err != nil {
		os.Remove(outPath)
		return nil, err
	}
	defer os.Remove(outPath)

	args := []string{"thumbnail", srcPath, outPath, fmt.Sprintf("%d", params.Width), "--height", fmt.Sprintf("%d", params.Height)}
	if params.Crop {
		args = append(args, "--crop", "centre")
	} else {
		args = append(args, "--size", "force")
	}
	cmd := exec.Command("vips", args...)
	if combined, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("command failed: %w: %s", err, strings.TrimSpace(string(combined)))
	}

	return os.ReadFile(outPath)
}

func resizeWithMagick(params ResizeParams) ([]byte, error) {
	inFmt, err := magickFormat(params.Ext)
	if err != nil {
		return nil, err
	}
	outFmt, err := magickFormat(params.Ext)
	if err != nil {
		return nil, err
	}

	args := []string{inFmt + ":-"}
	if params.Crop {
		size := fmt.Sprintf("%dx%d", params.Width, params.Height)
		args = append(args, "-resize", size+"^", "-gravity", "center", "-extent", size)
	} else {
		args = append(args, "-resize", fmt.Sprintf("%dx%d!", params.Width, params.Height))
	}
	cmd := exec.Command("magick", append(args, outFmt+":-")...)
	cmd.Stdin = bytes.NewReader(params.Input)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = "no stderr"
		}
		return nil, fmt.Errorf("command failed: %w: %s", err, msg)
	}

	if stdout.Len() == 0 {
		return nil, fmt.Errorf("command produced empty output")
	}

	return stdout.Bytes(), nil
}

func magickFormat(ext string) (string, error) {
	switch strings.ToLower(strings.TrimPrefix(ext, ".")) {
	case "jpg", "jpeg":
		return "jpeg", nil
	case "png":
		return "png", nil
	case "gif":
		return "gif", nil
	case "webp":
		return "webp", nil
	default:
		return "", fmt.Errorf("unsupported image format: %s", ext)
	}
}

// centreCrop returns the largest centred sub-rectangle of src with the aspect
// ratio of w x h.
func centreCrop(src image.Rectangle, w, h int) image.Rectangle {
	sw, sh := src.Dx(), src.Dy()
	cw, ch := sw, sh
	if sw*h > sh*w { // source is wider than the target
		cw = max(1, sh*w/h)
	} else {
		ch = max(1, sw*h/w)
	}
	x0 := src.Min.X + (sw-cw)/2
	y0 := src.Min.Y + (sh-ch)/2
	return image.Rect(x0, y0, x0+cw, y0+ch)
}

func resizeWithGo(params ResizeParams) ([]byte, error) {
	srcImg, _, err := image.Decode(bytes.NewReader(params.Input))
	if err != nil {
		return nil, err
	}

	srcRect := srcImg.Bounds()
	if params.Crop {
		srcRect = centreCrop(srcRect, params.Width, params.Height)
	}
	dstImg := image.NewRGBA(image.Rect(0, 0, params.Width, params.Height))
	draw.CatmullRom.Scale(dstImg, dstImg.Bounds(), srcImg, srcRect, draw.Over, nil)

	return encodeImage(dstImg, params.Ext)
}

func encodeImage(img image.Image, ext string) ([]byte, error) {
	out := &bytes.Buffer{}
	var err error
	switch strings.ToLower(ext) {
	case ".jpg", ".jpeg":
		err = jpeg.Encode(out, img, &jpeg.Options{Quality: 85})
	case ".gif":
		err = gif.Encode(out, img, nil)
	case ".png":
		err = png.Encode(out, img)
	default:
		return nil, fmt.Errorf("cannot encode output format: %s", ext)
	}
	if err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
