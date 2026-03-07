package imageproc

import (
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/image/draw"
)

type Metadata struct {
	Width  int
	Height int
	Format string
}

type Processor struct {
	BasePath   string
	OutputPath string
}

type ResizeParams struct {
	SrcPath string
	OutPath string
	Width   int
	Height  int
}

type resizeBackend struct {
	name string
	run  func(ResizeParams) error
}

var configuredResizeBackends = func() []resizeBackend {
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

func New(basePath string, outputPath string) *Processor {
	return &Processor{BasePath: basePath, OutputPath: outputPath}
}

func (p *Processor) GetMetadata(relPath string) (Metadata, error) {
	abs := filepath.Join(p.BasePath, relPath)
	f, err := os.Open(abs)
	if err != nil {
		return Metadata{}, err
	}
	defer f.Close()
	conf, format, err := image.DecodeConfig(f)
	if err != nil {
		return Metadata{}, err
	}
	return Metadata{Width: conf.Width, Height: conf.Height, Format: format}, nil
}

func (p *Processor) Resize(relPath string, width int, height int) (string, error) {
	if width <= 0 || height <= 0 {
		return "", fmt.Errorf("resize dimensions must be > 0")
	}
	srcPath := filepath.Join(p.BasePath, relPath)
	name := strings.TrimSuffix(filepath.Base(relPath), filepath.Ext(relPath))
	ext := strings.ToLower(filepath.Ext(relPath))
	if ext == "" {
		ext = ".png"
	}
	outRel := filepath.ToSlash(filepath.Join("processed_images", fmt.Sprintf("%s-%dx%d%s", name, width, height, ext)))
	outPath := filepath.Join(p.OutputPath, filepath.FromSlash(outRel))
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return "", err
	}

	params := ResizeParams{
		SrcPath: srcPath,
		OutPath: outPath,
		Width:   width,
		Height:  height,
	}
	if err := resizeWithBackends(configuredResizeBackends, params); err != nil {
		return "", err
	}

	return "/" + outRel, nil
}

func resizeWithBackends(backends []resizeBackend, params ResizeParams) error {
	if len(backends) == 0 {
		return fmt.Errorf("no resize backends configured")
	}

	errMsgs := make([]string, 0, len(backends))
	for _, backend := range backends {
		if err := backend.run(params); err != nil {
			errMsgs = append(errMsgs, fmt.Sprintf("%s: %v", backend.name, err))
			continue
		}
		return nil
	}

	return fmt.Errorf("all resize backends failed: %s", strings.Join(errMsgs, "; "))
}

func resizeWithVips(params ResizeParams) error {
	cmd := exec.Command(
		"vips",
		"thumbnail",
		params.SrcPath,
		params.OutPath,
		fmt.Sprintf("%d", params.Width),
		"--height",
		fmt.Sprintf("%d", params.Height),
		"--size",
		"force",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("command failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func resizeWithMagick(params ResizeParams) error {
	cmd := exec.Command(
		"magick",
		params.SrcPath,
		"-resize",
		fmt.Sprintf("%dx%d!", params.Width, params.Height),
		params.OutPath,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("command failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func resizeWithGo(params ResizeParams) error {
	srcFile, err := os.Open(params.SrcPath)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	srcImg, format, err := image.Decode(srcFile)
	if err != nil {
		return err
	}

	dstImg := image.NewRGBA(image.Rect(0, 0, params.Width, params.Height))
	draw.CatmullRom.Scale(dstImg, dstImg.Bounds(), srcImg, srcImg.Bounds(), draw.Over, nil)

	outFile, err := os.Create(params.OutPath)
	if err != nil {
		return err
	}
	defer outFile.Close()

	ext := strings.ToLower(filepath.Ext(params.OutPath))
	switch {
	case ext == ".jpg" || ext == ".jpeg" || format == "jpeg":
		return jpeg.Encode(outFile, dstImg, &jpeg.Options{Quality: 85})
	default:
		return png.Encode(outFile, dstImg)
	}
}
