package ui

import (
	"bytes"
	_ "embed"
	"image/png"

	"github.com/pwiecz/go-fltk"
)

//go:embed assets/foldersnap-icon.png
var folderSnapIconPNG []byte

func newFolderSnapIcon() (*fltk.RgbImage, error) {
	decoded, err := png.Decode(bytes.NewReader(folderSnapIconPNG))
	if err != nil {
		return nil, err
	}
	return fltk.NewRgbImageFromImage(decoded)
}
