// fbinfogrid project main src

// Copyright ©2020 Steve Merrony

// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.

// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

// Old Advice: Must do this first: sudo fbset -fb /dev/fb0 -depth 16 unless 16-bit framebuffer depth is set in config.txt
// Since late 2015 the default framebuffer depth under Raspbian is 32 bits, this yields the best performance.

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/disintegration/imaging"
	"github.com/golang/freetype"
	"github.com/golang/freetype/truetype"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

const (
	defaultConfig      = "config.json"
	defaultFont        = "LeagueMono-Regular.ttf"
	defaultFramebuffer = "/dev/fb0"
)

// N.B. In the following 3 types the exported fields may be unmarshalled from the JSON
//      configuration file, non-exported fields are for internal use only.

// ConfigT holds an fbinfogrid configuration (one or more Pages)
type ConfigT struct {
	Pages         []PageT
	currentPageIx int
}

// PageT describes the contents of a fbinfogrid page (display)
type PageT struct {
	Name                  string
	Rows, Cols            int
	Cells                 []CellT
	FontFile              string
	DurationMins          int
	cellWidth, cellHeight int
	font                  *truetype.Font
}

// CellT describes a piece of information on a page
type CellT *struct {
	Row, Col         int
	Rowspan, Colspan int
	RefreshSecs      int
	CellType         string
	Source, Text     string
	Sources          []string
	FontPts          float64
	fn               func(*sync.WaitGroup, *sync.Mutex, draw.Image, CellT)
	font             *truetype.Font
	format           string // used by the date/time funcs
	currentSrcIx     int
	positionRect     image.Rectangle
	picture          *image.NRGBA // .RGBA
}

// program arguments
var (
	configFlag = flag.String("config", defaultConfig, "JSON file describing the information layout")
	fbdevFlag  = flag.String("fbdev", defaultFramebuffer, "framebuffer device file")
)

func main() {
	flag.Parse()

	fb, err := Open(*fbdevFlag)
	if err != nil {
		panic(err)
	}
	bounds := fb.Bounds()

	var (
		updateMu sync.Mutex
		wg       sync.WaitGroup
		config   ConfigT
		stoppers []chan bool
	)

	config.loadConfig(*configFlag)

	config.currentPageIx = -1
	for {
		if config.currentPageIx++; config.currentPageIx == len(config.Pages) {
			config.currentPageIx = 0
		}
		page := config.Pages[config.currentPageIx]

		if page.FontFile == "" {
			page.FontFile = defaultFont
		}

		page.cellWidth = bounds.Dx() / page.Cols
		page.cellHeight = bounds.Dy() / page.Rows
		fmt.Printf("Page size in pixels is: %d x %d (w x h)\n", bounds.Dx(), bounds.Dy())
		fmt.Printf("Calculated cell size is: %d x %d (w x h)\n", page.cellWidth, page.cellHeight)

		bg := image.Black
		draw.Draw(fb, bounds, bg, image.ZP, draw.Src)
		page.font = loadFont(page.FontFile)

		for _, cell := range page.Cells {
			prepareCell(page, cell)
			stopper := startOrExecute(&wg, &updateMu, fb, cell)
			if stopper != nil {
				stoppers = append(stoppers, stopper)
			}
		}

		if len(config.Pages) > 1 && page.DurationMins > 0 {
			time.Sleep(time.Minute * time.Duration(page.DurationMins))
			for _, s := range stoppers {
				s <- true
			}
		}

		wg.Wait()
		stoppers = nil
	}
	//black := image.NewUniform(color.Black)
	//draw.Draw(fb, bounds, black, image.ZP, draw.Src)
}

func prepareCell(page PageT, cell CellT) {
	topLeftX := (cell.Col - 1) * page.cellWidth
	topLeftY := (cell.Row - 1) * page.cellHeight
	if cell.Rowspan == 0 {
		cell.Rowspan = 1
	}
	if cell.Colspan == 0 {
		cell.Colspan = 1
	}
	// calculate where and how big it will be drawn
	cell.positionRect = image.Rect(topLeftX, topLeftY, topLeftX+(page.cellWidth*cell.Colspan), topLeftY+(page.cellHeight*cell.Rowspan))
	cell.picture = image.NewNRGBA(image.Rect(0, 0, page.cellWidth*cell.Colspan, page.cellHeight*cell.Rowspan))
	cell.font = page.font

	switch cell.CellType {
	case "carousel":
		cell.currentSrcIx = -1
		cell.fn = drawCarousel
	case "datemonth":
		if cell.FontPts == 0.0 {
			cell.FontPts = 80.0
		}
		cell.format = "2 Jan"
		cell.fn = drawTime
	case "day":
		if cell.FontPts == 0.0 {
			cell.FontPts = 80.0
		}
		cell.format = "Mon"
		cell.fn = drawTime
	case "daydatemonth":
		if cell.FontPts == 0.0 {
			cell.FontPts = 80.0
		}
		cell.format = "Mon 2 Jan"
		cell.fn = drawTime
	case "hostname":
		if cell.FontPts == 0.0 {
			cell.FontPts = 80.0
		}
		cell.Text, _ = os.Hostname()
		cell.fn = drawText
	case "isalive":
		if cell.RefreshSecs == 0 {
			panic("Must set refreshsecs for cell type isalive")
		}
		if cell.FontPts == 0.0 {
			cell.FontPts = 60.0
		}
		if cell.Text == "" {
			cell.Text = strings.Split(cell.Source, ":")[0]
		}
		cell.fn = drawIsAlive
	case "localimage":
		cell.fn = drawLocalImage
	case "text":
		if cell.FontPts == 0.0 {
			cell.FontPts = 80.0
		}
		cell.fn = drawText
	case "time":
		if cell.FontPts == 0.0 {
			cell.FontPts = 128.0
		}
		cell.format = "15:04"
		cell.fn = drawTime
	case "urlimage":
		cell.fn = drawURLImage
	}
}

// funcs for handling each cell type

// drawCarousel goroutine to show rotating selection of images indefinitely
func drawCarousel(wg *sync.WaitGroup, updateMu *sync.Mutex, fb draw.Image, cell CellT) {
	if cell.currentSrcIx++; cell.currentSrcIx == len(cell.Sources) {
		cell.currentSrcIx = 0
	}
	i, err := os.Open(cell.Sources[cell.currentSrcIx])
	if err != nil {
		panic(err)
	}
	drawImage(i, cell, updateMu, fb)
	i.Close()
}

// drawIsAlive displays an indicator that a host is accessible
func drawIsAlive(wg *sync.WaitGroup, updateMu *sync.Mutex, fb draw.Image, cell CellT) {
	red := image.NewUniform(color.RGBA{255, 0, 0, 255})
	green := image.NewUniform(color.RGBA{0, 255, 0, 255})
	c, err := net.DialTimeout("tcp", cell.Source, time.Second*time.Duration(cell.RefreshSecs))
	if err != nil {
		draw.Draw(cell.picture, cell.picture.Bounds(), red, image.ZP, draw.Src)
	} else {
		c.Close()
		draw.Draw(cell.picture, cell.picture.Bounds(), green, image.ZP, draw.Src)
	}
	updateMu.Lock()
	writeText(cell.font, cell.FontPts, cell.picture, cell.Text)
	draw.Draw(fb, cell.positionRect, cell.picture, image.ZP, draw.Src)
	updateMu.Unlock()
}

// drawLocalImage displays an image from the filesystem
func drawLocalImage(wg *sync.WaitGroup, updateMu *sync.Mutex, fb draw.Image, cell CellT) {
	i, err := os.Open(cell.Source)
	if err != nil {
		panic(err)
	}
	drawImage(i, cell, updateMu, fb)
	i.Close()
}

//drawText displays the cell's current text
func drawText(wg *sync.WaitGroup, updateMu *sync.Mutex, fb draw.Image, cell CellT) {
	updateMu.Lock()
	writeText(cell.font, cell.FontPts, cell.picture, cell.Text)
	draw.Draw(fb, cell.positionRect, cell.picture, image.ZP, draw.Src)
	updateMu.Unlock()
}

// drawTime displays the currnent time using the supplied format
func drawTime(wg *sync.WaitGroup, updateMu *sync.Mutex, fb draw.Image, cell CellT) {
	timeStr := time.Now().Format(cell.format)
	updateMu.Lock()
	draw.Draw(cell.picture, cell.picture.Bounds(), image.Black, image.ZP, draw.Src)
	writeText(cell.font, cell.FontPts, cell.picture, timeStr)
	draw.Draw(fb, cell.positionRect, cell.picture, image.ZP, draw.Src)
	updateMu.Unlock()
}

// drawURLImage displays a remote image
func drawURLImage(wg *sync.WaitGroup, updateMu *sync.Mutex, fb draw.Image, cell CellT) {
	i, err := http.Get(cell.Source)
	if err == nil { // ignore errors here
		drawImage(i.Body, cell, updateMu, fb)
		i.Body.Close()
	}
}

// helper funcs

// drawImage copies the cell's image into the target image (eg. framebuffer)
func drawImage(img io.Reader, cell CellT, updateMu *sync.Mutex, fb draw.Image) {
	sImg, _, err := image.Decode(img)
	if err != nil {
		panic(err)
	}
	w := cell.picture.Bounds().Dx()
	h := cell.picture.Bounds().Dy()
	// sImg = imaging.Fit(sImg, w, h, imaging.NearestNeighbor)
	// sp := image.Point{
	// 	X: (sImg.Bounds().Dx() / 2) - (w / 2),
	// 	Y: (sImg.Bounds().Dy() / 2) - (h / 2),
	// }
	sImg = imaging.Fill(sImg, w, h, imaging.Center, imaging.NearestNeighbor)
	sp := image.Point{0, 0}
	updateMu.Lock()
	draw.Draw(fb, cell.positionRect, sImg, sp, draw.Src)
	updateMu.Unlock()
}

func (config *ConfigT) loadConfig(configFilename string) {
	configFile, err := os.Open(configFilename)
	if err != nil {
		panic(err)
	}
	defer configFile.Close()
	configJSON, err := ioutil.ReadAll(configFile)
	if err != nil {
		panic(err)
	}
	err = json.Unmarshal(configJSON, config)
	if err != nil {
		panic(err)
	}
}

func loadFont(fontFile string) *truetype.Font {
	fontBytes, err := ioutil.ReadFile(fontFile)
	if err != nil {
		panic(err)
	}
	font, err := freetype.ParseFont(fontBytes)
	if err != nil {
		panic(err)
	}
	return font
}

func startOrExecute(wg *sync.WaitGroup, updateMu *sync.Mutex, fb draw.Image, cell CellT) (stop chan bool) {
	if cell.RefreshSecs == 0 {
		// one-shot execute
		cell.fn(wg, updateMu, fb, cell)
		return nil
	}
	// regular execution
	cell.fn(wg, updateMu, fb, cell)
	ticker := time.NewTicker(time.Second * time.Duration(cell.RefreshSecs))
	stop = make(chan bool)
	go func() { //wg *sync.WaitGroup, updateMu *sync.Mutex, fb draw.Image) { //}, cell CellT) {
		for {
			select {
			case <-stop:
				wg.Done()
				return
			case <-ticker.C:
				cell.fn(wg, updateMu, fb, cell)
			}
		}
	}() //wg, updateMu, fb, cell)
	wg.Add(1)
	return stop
}

// writeText puts a short string on an image
func writeText(tfont *truetype.Font, pts float64, img draw.Image, text string) {
	d := &font.Drawer{
		Dst: img,
		Src: image.White,
		Face: truetype.NewFace(tfont, &truetype.Options{
			Size:    pts,
			Hinting: font.HintingFull,
		}),
	}
	textBounds, _ := d.BoundString(text)
	// fmt.Printf("Bounds for %s are: %v\n", text, textBounds)
	w := textBounds.Max.X - textBounds.Min.X
	h := textBounds.Max.Y - textBounds.Min.Y
	d.Dot = fixed.Point26_6{
		X: fixed.I(img.Bounds().Dx()/2) - (w / 2),
		Y: fixed.I(img.Bounds().Dy()/2) + (h / 2),
	}
	d.DrawString(text)
}
