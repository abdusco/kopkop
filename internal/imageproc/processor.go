package imageproc

import (
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"os"
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
	srcFile, err := os.Open(srcPath)
	if err != nil {
		return "", err
	}
	defer srcFile.Close()

	srcImg, format, err := image.Decode(srcFile)
	if err != nil {
		return "", err
	}

	dstImg := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.CatmullRom.Scale(dstImg, dstImg.Bounds(), srcImg, srcImg.Bounds(), draw.Over, nil)

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
	outFile, err := os.Create(outPath)
	if err != nil {
		return "", err
	}
	defer outFile.Close()

	switch {
	case ext == ".jpg" || ext == ".jpeg" || format == "jpeg":
		err = jpeg.Encode(outFile, dstImg, &jpeg.Options{Quality: 85})
	default:
		err = png.Encode(outFile, dstImg)
	}
	if err != nil {
		return "", err
	}

	return "/" + outRel, nil
}
