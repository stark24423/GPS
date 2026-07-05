package ui

import (
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"net/http"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"

	"gpssim/internal/core"
)

const (
	tileSize = 256
	minZoom  = 2
	maxZoom  = 19
)

type tileKey struct {
	Z int
	X int
	Y int
}

type MapWidget struct {
	widget.BaseWidget

	mu            sync.RWMutex
	center        core.Coordinate
	zoom          int
	renderSize    fyne.Size
	points        []core.Coordinate
	current       *core.Coordinate
	currentStatus string
	editingLocked bool
	followMode    bool

	onPoint func(core.Coordinate)

	tileMu  sync.Mutex
	tiles   map[tileKey]image.Image
	loading map[tileKey]bool
	client  http.Client
}

func NewMapWidget(onPoint func(core.Coordinate)) *MapWidget {
	m := &MapWidget{
		center:  core.Coordinate{Lat: 24.7808548, Lon: 121.0252718},
		zoom:    17,
		onPoint: onPoint,
		tiles:   make(map[tileKey]image.Image),
		loading: make(map[tileKey]bool),
		client:  http.Client{Timeout: 8 * time.Second},
	}
	m.ExtendBaseWidget(m)
	return m
}

func (m *MapWidget) SetPoints(points []core.Coordinate) {
	m.mu.Lock()
	m.points = append([]core.Coordinate(nil), points...)
	m.mu.Unlock()
	m.Refresh()
}

func (m *MapWidget) ClearPoints() {
	m.mu.Lock()
	m.points = nil
	m.mu.Unlock()
	m.Refresh()
}

func (m *MapWidget) SetCurrentPosition(point core.Coordinate, status string) {
	m.mu.Lock()
	m.current = &point
	m.currentStatus = status
	if m.followMode {
		m.center = point
	}
	m.mu.Unlock()
	m.Refresh()
}

func (m *MapWidget) CenterOn(point core.Coordinate) {
	m.mu.Lock()
	m.center = point
	m.mu.Unlock()
	m.Refresh()
}

func (m *MapWidget) ClearCurrentPosition() {
	m.mu.Lock()
	m.current = nil
	m.currentStatus = ""
	m.mu.Unlock()
	m.Refresh()
}

func (m *MapWidget) SetEditingLocked(locked bool) {
	m.mu.Lock()
	m.editingLocked = locked
	m.mu.Unlock()
}

func (m *MapWidget) SetFollowMode(enabled bool) {
	m.mu.Lock()
	m.followMode = enabled
	if enabled && m.current != nil {
		m.center = *m.current
	}
	m.mu.Unlock()
	m.Refresh()
}

func (m *MapWidget) ZoomIn() {
	m.mu.Lock()
	if m.zoom < maxZoom {
		m.zoom++
	}
	m.mu.Unlock()
	m.Refresh()
}

func (m *MapWidget) ZoomOut() {
	m.mu.Lock()
	if m.zoom > minZoom {
		m.zoom--
	}
	m.mu.Unlock()
	m.Refresh()
}

func (m *MapWidget) Scrolled(event *fyne.ScrollEvent) {
	if event.Scrolled.DY == 0 {
		return
	}

	pos, size := m.eventRenderGeometry(event.Position)
	m.zoomAt(pos, size, event.Scrolled.DY > 0)
}

func (m *MapWidget) Tapped(event *fyne.PointEvent) {
	m.mu.RLock()
	locked := m.editingLocked
	m.mu.RUnlock()
	if locked || m.onPoint == nil {
		return
	}
	pos, size := m.eventRenderGeometry(event.Position)
	m.onPoint(m.coordinateAt(pos, size))
}

func (m *MapWidget) Dragged(event *fyne.DragEvent) {
	m.mu.Lock()
	dx, dy := m.scaledDeltaLocked(event.Dragged)
	centerX, centerY := latLonToWorld(m.center.Lat, m.center.Lon, m.zoom)
	centerX -= float64(dx)
	centerY -= float64(dy)
	lat, lon := worldToLatLon(centerX, centerY, m.zoom)
	m.center = core.Coordinate{Lat: lat, Lon: lon}
	m.followMode = false
	m.mu.Unlock()
	m.Refresh()
}

func (m *MapWidget) DragEnd() {}

func (m *MapWidget) zoomAt(pos fyne.Position, size fyne.Size, zoomIn bool) {
	m.mu.Lock()
	oldZoom := m.zoom
	newZoom := oldZoom
	if zoomIn && newZoom < maxZoom {
		newZoom++
	}
	if !zoomIn && newZoom > minZoom {
		newZoom--
	}
	if newZoom == oldZoom {
		m.mu.Unlock()
		return
	}

	centerX, centerY := latLonToWorld(m.center.Lat, m.center.Lon, oldZoom)
	anchorX := centerX + float64(pos.X-size.Width/2)
	anchorY := centerY + float64(pos.Y-size.Height/2)
	anchorLat, anchorLon := worldToLatLon(anchorX, anchorY, oldZoom)

	nextAnchorX, nextAnchorY := latLonToWorld(anchorLat, anchorLon, newZoom)
	nextCenterX := nextAnchorX - float64(pos.X-size.Width/2)
	nextCenterY := nextAnchorY - float64(pos.Y-size.Height/2)
	lat, lon := worldToLatLon(nextCenterX, nextCenterY, newZoom)

	m.center = core.Coordinate{Lat: lat, Lon: lon}
	m.zoom = newZoom
	m.mu.Unlock()
	m.Refresh()
}

func (m *MapWidget) CreateRenderer() fyne.WidgetRenderer {
	raster := canvas.NewRaster(func(width, height int) image.Image {
		m.setRenderSize(width, height)
		return m.render(width, height)
	})
	return &mapRenderer{raster: raster}
}

func (m *MapWidget) setRenderSize(width, height int) {
	m.mu.Lock()
	m.renderSize = fyne.NewSize(float32(width), float32(height))
	m.mu.Unlock()
}

func (m *MapWidget) eventRenderGeometry(pos fyne.Position) (fyne.Position, fyne.Size) {
	m.mu.RLock()
	widgetSize := m.Size()
	renderSize := m.renderSize
	m.mu.RUnlock()

	if renderSize.Width <= 0 || renderSize.Height <= 0 || widgetSize.Width <= 0 || widgetSize.Height <= 0 {
		return pos, widgetSize
	}
	scaleX := renderSize.Width / widgetSize.Width
	scaleY := renderSize.Height / widgetSize.Height
	return fyne.NewPos(pos.X*scaleX, pos.Y*scaleY), renderSize
}

func (m *MapWidget) scaledDeltaLocked(delta fyne.Delta) (float32, float32) {
	widgetSize := m.Size()
	renderSize := m.renderSize
	if renderSize.Width <= 0 || renderSize.Height <= 0 || widgetSize.Width <= 0 || widgetSize.Height <= 0 {
		return delta.DX, delta.DY
	}
	return delta.DX * renderSize.Width / widgetSize.Width, delta.DY * renderSize.Height / widgetSize.Height
}

func (m *MapWidget) coordinateAt(pos fyne.Position, size fyne.Size) core.Coordinate {
	m.mu.RLock()
	center := m.center
	zoom := m.zoom
	m.mu.RUnlock()

	centerX, centerY := latLonToWorld(center.Lat, center.Lon, zoom)
	worldX := centerX + float64(pos.X-size.Width/2)
	worldY := centerY + float64(pos.Y-size.Height/2)
	lat, lon := worldToLatLon(worldX, worldY, zoom)
	return core.Coordinate{Lat: lat, Lon: lon}
}

func (m *MapWidget) render(width, height int) image.Image {
	if width <= 0 || height <= 0 {
		return image.NewRGBA(image.Rect(0, 0, 1, 1))
	}

	m.mu.RLock()
	center := m.center
	zoom := m.zoom
	points := append([]core.Coordinate(nil), m.points...)
	var current *core.Coordinate
	if m.current != nil {
		cp := *m.current
		current = &cp
	}
	locked := m.editingLocked
	m.mu.RUnlock()

	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(dst, dst.Bounds(), &image.Uniform{C: color.RGBA{R: 241, G: 245, B: 249, A: 255}}, image.Point{}, draw.Src)

	centerX, centerY := latLonToWorld(center.Lat, center.Lon, zoom)
	topLeftX := centerX - float64(width)/2
	topLeftY := centerY - float64(height)/2
	m.drawTiles(dst, zoom, topLeftX, topLeftY)

	if len(points) > 1 {
		for i := 0; i < len(points)-1; i++ {
			x1, y1 := m.screenPoint(points[i], zoom, topLeftX, topLeftY)
			x2, y2 := m.screenPoint(points[i+1], zoom, topLeftX, topLeftY)
			drawLine(dst, x1, y1, x2, y2, color.RGBA{R: 37, G: 99, B: 235, A: 255}, 4)
		}
	}

	for _, point := range points {
		x, y := m.screenPoint(point, zoom, topLeftX, topLeftY)
		drawCircle(dst, x, y, 8, color.RGBA{R: 255, G: 255, B: 255, A: 255})
		drawCircle(dst, x, y, 6, color.RGBA{R: 37, G: 99, B: 235, A: 255})
	}

	if current != nil {
		x, y := m.screenPoint(*current, zoom, topLeftX, topLeftY)
		drawCircle(dst, x, y, 13, color.RGBA{R: 3, G: 105, B: 161, A: 150})
		drawCircle(dst, x, y, 9, color.RGBA{R: 255, G: 255, B: 255, A: 255})
		drawCircle(dst, x, y, 6, color.RGBA{R: 14, G: 165, B: 233, A: 255})
	}

	if locked {
		fillRect(dst, 0, 0, width, 4, color.RGBA{R: 34, G: 197, B: 94, A: 255})
	}

	return dst
}

func (m *MapWidget) drawTiles(dst *image.RGBA, zoom int, topLeftX, topLeftY float64) {
	width := dst.Bounds().Dx()
	height := dst.Bounds().Dy()
	minX := int(math.Floor(topLeftX / tileSize))
	maxX := int(math.Floor((topLeftX + float64(width)) / tileSize))
	minY := int(math.Floor(topLeftY / tileSize))
	maxY := int(math.Floor((topLeftY + float64(height)) / tileSize))
	tileCount := 1 << zoom

	for ty := minY; ty <= maxY; ty++ {
		if ty < 0 || ty >= tileCount {
			continue
		}
		for tx := minX; tx <= maxX; tx++ {
			wrappedX := ((tx % tileCount) + tileCount) % tileCount
			key := tileKey{Z: zoom, X: wrappedX, Y: ty}
			screenX := int(float64(tx*tileSize) - topLeftX)
			screenY := int(float64(ty*tileSize) - topLeftY)
			tile := m.getTile(key)
			if tile == nil {
				drawTilePlaceholder(dst, screenX, screenY)
				continue
			}
			draw.Draw(dst, image.Rect(screenX, screenY, screenX+tileSize, screenY+tileSize), tile, image.Point{}, draw.Over)
		}
	}
}

func (m *MapWidget) getTile(key tileKey) image.Image {
	m.tileMu.Lock()
	defer m.tileMu.Unlock()

	if tile, ok := m.tiles[key]; ok {
		return tile
	}
	if !m.loading[key] {
		m.loading[key] = true
		go m.loadTile(key)
	}
	return nil
}

func (m *MapWidget) loadTile(key tileKey) {
	url := "https://tile.openstreetmap.org/" +
		itoa(key.Z) + "/" + itoa(key.X) + "/" + itoa(key.Y) + ".png"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		m.finishTile(key, nil)
		return
	}
	req.Header.Set("User-Agent", "gpssim-go-fyne/1.0")

	resp, err := m.client.Do(req)
	if err != nil {
		m.finishTile(key, nil)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		m.finishTile(key, nil)
		return
	}

	img, _, err := image.Decode(resp.Body)
	if err != nil {
		m.finishTile(key, nil)
		return
	}
	m.finishTile(key, img)
}

func (m *MapWidget) finishTile(key tileKey, img image.Image) {
	m.tileMu.Lock()
	if img != nil {
		m.tiles[key] = img
	}
	delete(m.loading, key)
	m.tileMu.Unlock()

	fyne.Do(func() {
		m.Refresh()
	})
}

func (m *MapWidget) screenPoint(point core.Coordinate, zoom int, topLeftX, topLeftY float64) (int, int) {
	x, y := latLonToWorld(point.Lat, point.Lon, zoom)
	return int(math.Round(x - topLeftX)), int(math.Round(y - topLeftY))
}

type mapRenderer struct {
	raster *canvas.Raster
}

func (r *mapRenderer) Layout(size fyne.Size) {
	r.raster.Resize(size)
}

func (r *mapRenderer) MinSize() fyne.Size {
	return fyne.NewSize(520, 420)
}

func (r *mapRenderer) Refresh() {
	r.raster.Refresh()
}

func (r *mapRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.raster}
}

func (r *mapRenderer) Destroy() {}

func latLonToWorld(lat, lon float64, zoom int) (float64, float64) {
	lat = math.Max(-85.05112878, math.Min(85.05112878, lat))
	scale := float64(tileSize * (int(1) << zoom))
	x := (lon + 180) / 360 * scale
	latRad := lat * math.Pi / 180
	y := (1 - math.Log(math.Tan(latRad)+1/math.Cos(latRad))/math.Pi) / 2 * scale
	return x, y
}

func worldToLatLon(x, y float64, zoom int) (float64, float64) {
	scale := float64(tileSize * (int(1) << zoom))
	lon := core.NormalizeLongitude(x/scale*360 - 180)
	n := math.Pi - 2*math.Pi*y/scale
	lat := 180 / math.Pi * math.Atan(0.5*(math.Exp(n)-math.Exp(-n)))
	return lat, lon
}

func drawTilePlaceholder(dst *image.RGBA, x, y int) {
	fillRect(dst, x, y, tileSize, tileSize, color.RGBA{R: 226, G: 232, B: 240, A: 255})
	drawLine(dst, x, y, x+tileSize, y, color.RGBA{R: 203, G: 213, B: 225, A: 255}, 1)
	drawLine(dst, x, y, x, y+tileSize, color.RGBA{R: 203, G: 213, B: 225, A: 255}, 1)
}

func drawCircle(dst *image.RGBA, cx, cy, radius int, c color.RGBA) {
	r2 := radius * radius
	for y := -radius; y <= radius; y++ {
		for x := -radius; x <= radius; x++ {
			if x*x+y*y <= r2 {
				setPixel(dst, cx+x, cy+y, c)
			}
		}
	}
}

func drawLine(dst *image.RGBA, x0, y0, x1, y1 int, c color.RGBA, width int) {
	dx := math.Abs(float64(x1 - x0))
	dy := -math.Abs(float64(y1 - y0))
	sx := -1
	if x0 < x1 {
		sx = 1
	}
	sy := -1
	if y0 < y1 {
		sy = 1
	}
	err := dx + dy
	for {
		half := width / 2
		fillRect(dst, x0-half, y0-half, width, width, c)
		if x0 == x1 && y0 == y1 {
			break
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x0 += sx
		}
		if e2 <= dx {
			err += dx
			y0 += sy
		}
	}
}

func fillRect(dst *image.RGBA, x, y, width, height int, c color.RGBA) {
	rect := image.Rect(x, y, x+width, y+height).Intersect(dst.Bounds())
	if rect.Empty() {
		return
	}
	draw.Draw(dst, rect, &image.Uniform{C: c}, image.Point{}, draw.Over)
}

func setPixel(dst *image.RGBA, x, y int, c color.RGBA) {
	if !image.Pt(x, y).In(dst.Bounds()) {
		return
	}
	dst.SetRGBA(x, y, c)
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for value > 0 {
		i--
		buf[i] = byte('0' + value%10)
		value /= 10
	}
	return string(buf[i:])
}
