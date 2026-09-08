package style

import "github.com/gookit/color"

type Color struct {
	rgb   *color.RGBColor
	basic *color.Color
	// noPromote, when true, tells TextStyle.deriveStyle to render this
	// color through its own native SGR code (see deriveMixedStyle) instead
	// of promoting the whole style to a single shared color space when
	// paired with a color of a different kind on the other channel - see
	// NewFixedRGBColor.
	noPromote bool
}

func NewRGBColor(cl color.RGBColor) Color {
	c := Color{}
	c.rgb = &cl
	return c
}

// NewFixedRGBColor is like NewRGBColor, but pairing it with a basic color on
// the other channel will NOT force that basic color to be reinterpreted
// through gookit's fixed RGB approximation of it (see TextStyle.deriveStyle)
// - each channel keeps rendering through its own native SGR code instead.
// Useful for a background that needs a specific, precise color without
// recoloring basic-palette foreground text away from the terminal's own
// theme.
func NewFixedRGBColor(cl color.RGBColor) Color {
	c := Color{}
	c.rgb = &cl
	c.noPromote = true
	return c
}

func NewBasicColor(cl color.Color) Color {
	c := Color{}
	c.basic = &cl
	return c
}

func (c Color) IsRGB() bool {
	return c.rgb != nil
}

func (c Color) ToRGB(isBg bool) Color {
	if c.IsRGB() {
		return c
	}

	if isBg {
		// We need to convert bg color to fg color
		// This is a gookit/color bug,
		// https://github.com/gookit/color/issues/39
		return NewRGBColor((*c.basic - 10).RGB())
	}

	return NewRGBColor(c.basic.RGB())
}
