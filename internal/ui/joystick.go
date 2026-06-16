package ui

import (
	"image"
	"image/color"
	"image/draw"
	"math"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
)

type Joystick struct {
	widget.BaseWidget

	dx       float64
	dy       float64
	dragging bool
	onChange func(dx, dy float64)
}

func NewJoystick(onChange func(dx, dy float64)) *Joystick {
	j := &Joystick{onChange: onChange}
	j.ExtendBaseWidget(j)
	return j
}

func (j *Joystick) Reset() {
	j.dx = 0
	j.dy = 0
	j.dragging = false
	if j.onChange != nil {
		j.onChange(0, 0)
	}
	j.Refresh()
}

func (j *Joystick) Dragged(event *fyne.DragEvent) {
	size := j.Size()
	centerX := float64(size.Width / 2)
	centerY := float64(size.Height / 2)
	x := float64(event.Position.X) - centerX
	y := float64(event.Position.Y) - centerY
	radius := math.Min(float64(size.Width), float64(size.Height))/2 - 8
	if radius <= 0 {
		return
	}
	distance := math.Hypot(x, y)
	if distance > radius {
		x = x / distance * radius
		y = y / distance * radius
	}

	dx := x / radius
	dy := -y / radius
	if math.Hypot(dx, dy) < 0.06 {
		dx = 0
		dy = 0
	}
	j.setDirection(dx, dy)
}

func (j *Joystick) DragEnd() {
	j.Reset()
}

func (j *Joystick) Tapped(event *fyne.PointEvent) {
	j.Dragged(&fyne.DragEvent{PointEvent: *event})
}

func (j *Joystick) setDirection(dx, dy float64) {
	if dx == j.dx && dy == j.dy {
		return
	}
	j.dx = dx
	j.dy = dy
	if j.onChange != nil {
		j.onChange(dx, dy)
	}
	j.Refresh()
}

func (j *Joystick) CreateRenderer() fyne.WidgetRenderer {
	raster := canvas.NewRaster(func(width, height int) image.Image {
		return j.render(width, height)
	})
	return &joystickRenderer{raster: raster}
}

func (j *Joystick) render(width, height int) image.Image {
	if width <= 0 || height <= 0 {
		return image.NewRGBA(image.Rect(0, 0, 1, 1))
	}
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(dst, dst.Bounds(), &image.Uniform{C: color.RGBA{R: 255, G: 255, B: 255, A: 255}}, image.Point{}, draw.Src)

	cx := width / 2
	cy := height / 2
	radius := int(math.Min(float64(width), float64(height))/2) - 10
	innerRadius := radius - 5
	knobRadius := int(float64(radius) * 0.28)
	kx := cx + int(j.dx*float64(radius))
	ky := cy - int(j.dy*float64(radius))

	drawCircle(dst, cx+1, cy+2, radius, color.RGBA{R: 203, G: 213, B: 225, A: 120})
	drawCircle(dst, cx, cy, radius, color.RGBA{R: 226, G: 232, B: 240, A: 255})
	drawCircle(dst, cx, cy, innerRadius, color.RGBA{R: 248, G: 250, B: 252, A: 255})
	drawCircleOutline(dst, cx, cy, innerRadius, color.RGBA{R: 203, G: 213, B: 225, A: 255})
	drawLine(dst, cx-innerRadius+12, cy, cx+innerRadius-12, cy, color.RGBA{R: 203, G: 213, B: 225, A: 255}, 1)
	drawLine(dst, cx, cy-innerRadius+12, cx, cy+innerRadius-12, color.RGBA{R: 203, G: 213, B: 225, A: 255}, 1)
	drawCircle(dst, cx, cy, 3, color.RGBA{R: 148, G: 163, B: 184, A: 255})

	drawCircle(dst, kx+1, ky+2, knobRadius, color.RGBA{R: 30, G: 41, B: 59, A: 90})
	drawCircle(dst, kx, ky, knobRadius, color.RGBA{R: 37, G: 99, B: 235, A: 255})
	drawCircle(dst, kx-3, ky-3, knobRadius/2, color.RGBA{R: 96, G: 165, B: 250, A: 255})
	drawCircleOutline(dst, kx, ky, knobRadius, color.RGBA{R: 30, G: 64, B: 175, A: 255})

	return dst
}

func drawCircleOutline(dst *image.RGBA, cx, cy, radius int, c color.RGBA) {
	r2 := radius * radius
	inner := radius - 2
	if inner < 0 {
		inner = 0
	}
	inner2 := inner * inner
	for y := -radius; y <= radius; y++ {
		for x := -radius; x <= radius; x++ {
			d := x*x + y*y
			if d <= r2 && d >= inner2 {
				setPixel(dst, cx+x, cy+y, c)
			}
		}
	}
}

type joystickRenderer struct {
	raster *canvas.Raster
}

func (r *joystickRenderer) Layout(size fyne.Size) {
	r.raster.Resize(size)
}

func (r *joystickRenderer) MinSize() fyne.Size {
	return fyne.NewSize(120, 120)
}

func (r *joystickRenderer) Refresh() {
	r.raster.Refresh()
}

func (r *joystickRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.raster}
}

func (r *joystickRenderer) Destroy() {}
