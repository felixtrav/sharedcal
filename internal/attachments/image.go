package attachments

import (
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"os"

	"github.com/disintegration/imaging"
	"golang.org/x/image/webp"
)

const (
	// DefaultMaxPixels is the decompression-bomb limit from the spec (40 MP).
	DefaultMaxPixels = 40_000_000
	// maxDimension rejects absurd aspect ratios (e.g. 1 x 40,000,000) that
	// pass the pixel limit but are not real photos or flyers.
	maxDimension = 30_000
	// ThumbSize is the longest side of generated thumbnails.
	ThumbSize   = 480
	jpegQuality = 85
)

// imageCodec bundles the explicit decoder for one detected format. Using the
// specific decoder (rather than image.Decode's own sniffing) guarantees we
// decode exactly the format mimetype detected.
type imageCodec struct {
	decodeConfig func(io.Reader) (image.Config, error)
	decode       func(io.Reader) (image.Image, error)
	// outJPEG selects the output encoding: JPEG stays JPEG, everything else
	// becomes PNG (GIF -> first frame, WebP -> PNG since Go has no pure-Go
	// WebP encoder).
	outJPEG bool
}

var codecs = map[string]imageCodec{
	"image/png":  {decodeConfig: png.DecodeConfig, decode: png.Decode},
	"image/apng": {decodeConfig: png.DecodeConfig, decode: png.Decode},
	"image/jpeg": {decodeConfig: jpeg.DecodeConfig, decode: decodeJPEGOriented, outJPEG: true},
	"image/gif":  {decodeConfig: gif.DecodeConfig, decode: gif.Decode}, // gif.Decode returns the first frame only
	"image/webp": {decodeConfig: webp.DecodeConfig, decode: webp.Decode},
}

// decodeJPEGOriented decodes a JPEG and applies its EXIF orientation. JPEG
// magic was already verified, so imaging's image.Decode sniff selects the
// JPEG decoder.
func decodeJPEGOriented(r io.Reader) (image.Image, error) {
	return imaging.Decode(r, imaging.AutoOrientation(true))
}

// encodedImage is a re-encoded image written to a temp file.
type encodedImage struct {
	spill         *spillFile
	contentType   string
	ext           string
	width, height int
}

// reencode decodes the spilled upload with a pixel limit and writes a clean
// copy plus a thumbnail. Re-encoding from raw pixels drops every metadata
// chunk (EXIF, GPS, XMP, ICC, comments) and any payload appended to or
// embedded in the file (polyglots), and the original bytes are discarded.
func (p *Pipeline) reencode(src *os.File, mime string) (full, thumb *encodedImage, err error) {
	codec, ok := codecs[mime]
	if !ok {
		return nil, nil, ErrTypeNotAllowed
	}
	if err := p.checkDimensions(src, codec); err != nil {
		return nil, nil, err
	}
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return nil, nil, err
	}
	img, err := safeDecode(codec.decode, src)
	if err != nil {
		return nil, nil, err
	}
	b := img.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		return nil, nil, ErrCorrupt
	}

	full, err = p.encodeImage(img, codec.outJPEG)
	if err != nil {
		return nil, nil, err
	}
	thumb, err = p.encodeImage(thumbnail(img), codec.outJPEG)
	if err != nil {
		full.spill.cleanup()
		return nil, nil, err
	}
	return full, thumb, nil
}

// checkDimensions reads only the image header and rejects decompression
// bombs before any pixel buffer is allocated.
func (p *Pipeline) checkDimensions(src *os.File, codec imageCodec) error {
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return err
	}
	cfg, err := codec.decodeConfig(src)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return ErrCorrupt
	}
	maxPixels := int64(p.MaxPixels)
	if maxPixels <= 0 {
		maxPixels = DefaultMaxPixels
	}
	if cfg.Width > maxDimension || cfg.Height > maxDimension ||
		int64(cfg.Width)*int64(cfg.Height) > maxPixels {
		return ErrImageTooLarge
	}
	return nil
}

// safeDecode converts decoder failures (and, defensively, panics from
// third-party decoders on hostile input) into ErrCorrupt.
func safeDecode(decode func(io.Reader) (image.Image, error), r io.Reader) (img image.Image, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			img, err = nil, fmt.Errorf("%w: decoder panic: %v", ErrCorrupt, rec)
		}
	}()
	img, err = decode(r)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	return img, nil
}

// thumbnail scales img so its longest side is ThumbSize, never upscaling.
func thumbnail(img image.Image) image.Image {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	if w <= ThumbSize && h <= ThumbSize {
		return img
	}
	tw, th := ThumbSize, ThumbSize
	if w >= h {
		th = max(1, int(int64(h)*ThumbSize/int64(w)))
	} else {
		tw = max(1, int(int64(w)*ThumbSize/int64(h)))
	}
	return imaging.Resize(img, tw, th, imaging.Lanczos)
}

// encodeImage writes img as JPEG (q85) or PNG to a hashed temp file.
func (p *Pipeline) encodeImage(img image.Image, asJPEG bool) (*encodedImage, error) {
	sf, err := newSpill(p.TempDir)
	if err != nil {
		return nil, err
	}
	out := &encodedImage{spill: sf, width: img.Bounds().Dx(), height: img.Bounds().Dy()}
	w := sf.writer()
	if asJPEG {
		out.contentType, out.ext = "image/jpeg", ".jpg"
		err = jpeg.Encode(w, img, &jpeg.Options{Quality: jpegQuality})
	} else {
		out.contentType, out.ext = "image/png", ".png"
		err = (&png.Encoder{CompressionLevel: png.DefaultCompression}).Encode(w, img)
	}
	if err != nil {
		sf.cleanup()
		return nil, fmt.Errorf("attachments: encode image: %w", err)
	}
	sf.finish()
	return out, nil
}
