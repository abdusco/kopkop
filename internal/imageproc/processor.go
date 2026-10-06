package imageproc

import (
	"bytes"
	"crypto/sha256"
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
	done chan struct{}
	url  string
	err  error
}

type ResizeParams struct {
	Input  []byte
	Ext    string
	Width  int
	Height int
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
	backends = append(backends, resizeBackend{name: "go", run: resizeWithGo})
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

func (p *Processor) Resize(relPath string, width int, height int) (string, error) {
	if width <= 0 || height <= 0 {
		return "", fmt.Errorf("resize dimensions must be > 0")
	}

	srcBytes, err := p.SourceFS.ReadFile(filepath.ToSlash(relPath))
	if err != nil {
		return "", err
	}

	ext := extensionFromImageData(srcBytes)
	if ext == "" {
		return "", fmt.Errorf("unable to detect image format for %q", relPath)
	}

	// Include the transformation version, format, dimensions, backend policy,
	// and source bytes. Identical transforms share a name regardless of path.
	digest := sha256.New()
	fmt.Fprintf(digest, "kopkop-resize-v1:%d:%d:%s:", width, height, ext)
	for _, backend := range p.backends {
		fmt.Fprintf(digest, "%s:", backend.name)
	}
	digest.Write(srcBytes)
	outRel := fmt.Sprintf("processed_images/%x-%dx%d%s", digest.Sum(nil), width, height, ext)
	p.mu.Lock()
	if result := p.resizes[outRel]; result != nil {
		p.mu.Unlock()
		<-result.done
		return result.url, result.err
	}
	result := &resizeResult{done: make(chan struct{})}
	p.resizes[outRel] = result
	p.mu.Unlock()
	result.url, result.err = p.resizeTo(outRel, ResizeParams{Input: srcBytes, Ext: ext, Width: width, Height: height})
	p.mu.Lock()
	if result.err != nil {
		delete(p.resizes, outRel) // Failed writes and transforms may be retried.
	}
	close(result.done)
	p.mu.Unlock()
	return result.url, result.err
}

func (p *Processor) resizeTo(outRel string, params ResizeParams) (string, error) {
	outBytes, err := resizeWithBackends(p.backends, params)
	if err != nil {
		return "", err
	}

	if err := p.OutputFS.MkdirAll(filepath.ToSlash(filepath.Dir(outRel)), 0o755); err != nil {
		return "", err
	}
	if err := p.OutputFS.WriteFile(outRel, outBytes, 0o644); err != nil {
		return "", err
	}

	return "/" + outRel, nil
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

	cmd := exec.Command(
		"vips",
		"thumbnail",
		srcPath,
		outPath,
		fmt.Sprintf("%d", params.Width),
		"--height",
		fmt.Sprintf("%d", params.Height),
		"--size",
		"force",
	)
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

	cmd := exec.Command(
		"magick",
		inFmt+":-",
		"-resize",
		fmt.Sprintf("%dx%d!", params.Width, params.Height),
		outFmt+":-",
	)
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

func resizeWithGo(params ResizeParams) ([]byte, error) {
	srcImg, _, err := image.Decode(bytes.NewReader(params.Input))
	if err != nil {
		return nil, err
	}

	dstImg := image.NewRGBA(image.Rect(0, 0, params.Width, params.Height))
	draw.CatmullRom.Scale(dstImg, dstImg.Bounds(), srcImg, srcImg.Bounds(), draw.Over, nil)

	out := &bytes.Buffer{}
	switch strings.ToLower(params.Ext) {
	case ".jpg", ".jpeg":
		if err := jpeg.Encode(out, dstImg, &jpeg.Options{Quality: 85}); err != nil {
			return nil, err
		}
	case ".gif":
		if err := gif.Encode(out, dstImg, nil); err != nil {
			return nil, err
		}
	case ".png":
		if err := png.Encode(out, dstImg); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("go resize backend does not support output format: %s", params.Ext)
	}

	return out.Bytes(), nil
}
