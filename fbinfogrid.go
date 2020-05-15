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
	"image/draw"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"io/ioutil"
	"net/http"
	"os"
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

// PageT describes the contents of a fbinfogrid page (display)
type PageT struct {
	Name       string
	Rows, Cols int
	Cells      []CellT
	FontFile   string
}

// CellT describes a piece of information on a page
type CellT struct {
	Row, Col         int
	Rowspan, Colspan int
	RefreshSecs      int
	CellType         string
	Source, Text     string
	Sources          []string
	FontPts          float64
	// private fields used in code...
	font         *truetype.Font
	positionRect image.Rectangle
	picture      *image.NRGBA // .RGBA
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

	var updateMu sync.Mutex
	var wg sync.WaitGroup

	var page PageT
	page.loadConfig(*configFlag)
	if page.FontFile == "" {
		page.FontFile = defaultFont
	}
	// fmt.Printf("Definition: for page %s is %v\n", page.Name, page)

	cellWidth := bounds.Dx() / page.Cols
	cellHeight := bounds.Dy() / page.Rows
	fmt.Printf("Page size in pixels is: %d x %d (w x h)\n", bounds.Dx(), bounds.Dy())
	fmt.Printf("Calculated cell size is: %d x %d (w x h)\n", cellWidth, cellHeight)

	bg := image.Black
	draw.Draw(fb, bounds, bg, image.ZP, draw.Src)

	font := loadFont(page.FontFile)

	//for {
	for _, cell := range page.Cells {
		topLeftX := (cell.Col - 1) * cellWidth
		topLeftY := (cell.Row - 1) * cellHeight

		if cell.Rowspan == 0 {
			cell.Rowspan = 1
		}
		if cell.Colspan == 0 {
			cell.Colspan = 1
		}
		// calculate where and how big it will be drawn
		cell.positionRect = image.Rect(topLeftX, topLeftY, topLeftX+(cellWidth*cell.Colspan), topLeftY+(cellHeight*cell.Rowspan))
		cell.picture = image.NewNRGBA(image.Rect(0, 0, cellWidth*cell.Colspan, cellHeight*cell.Rowspan))
		cell.font = font

		switch cell.CellType {
		case "carousel":
			wg.Add(1)
			go drawCarousel(&wg, &updateMu, fb, cell)
		case "datemonth":
			if cell.FontPts == 0.0 {
				cell.FontPts = 80.0
			}
			wg.Add(1)
			go drawTime(&wg, &updateMu, fb, cell, "2 Jan")
		case "day":
			if cell.FontPts == 0.0 {
				cell.FontPts = 80.0
			}
			wg.Add(1)
			go drawTime(&wg, &updateMu, fb, cell, "Mon")
		case "daydatemonth":
			if cell.FontPts == 0.0 {
				cell.FontPts = 80.0
			}
			wg.Add(1)
			go drawTime(&wg, &updateMu, fb, cell, "Mon 2 Jan")
		case "hostname":
			if cell.FontPts == 0.0 {
				cell.FontPts = 80.0
			}
			hn, _ := os.Hostname()
			updateMu.Lock()
			writeText(font, cell.FontPts, cell.picture, hn)
			draw.Draw(fb, cell.positionRect, cell.picture, image.ZP, draw.Src)
			updateMu.Unlock()
		case "localimage":
			wg.Add(1)
			go drawLocalImage(&wg, &updateMu, fb, cell)
		case "text":
			if cell.FontPts == 0.0 {
				cell.FontPts = 80.0
			}
			updateMu.Lock()
			writeText(font, cell.FontPts, cell.picture, cell.Text)
			draw.Draw(fb, cell.positionRect, cell.picture, image.ZP, draw.Src)
			updateMu.Unlock()
		case "time":
			if cell.FontPts == 0.0 {
				cell.FontPts = 128.0
			}
			wg.Add(1)
			go drawTime(&wg, &updateMu, fb, cell, "15:04")
		case "urlimage":
			wg.Add(1)
			go drawURLImage(&wg, &updateMu, fb, cell)
		}
	}

	wg.Wait()

	//}
	//black := image.NewUniform(color.Black)
	//draw.Draw(fb, bounds, black, image.ZP, draw.Src)
}

// funcs for handling each cell type

// drawCarousel goroutine to show rotating selection of images indefinitely
func drawCarousel(wg *sync.WaitGroup, updateMu *sync.Mutex, fb draw.Image, cell CellT) {
	ix := -1
	for {
		if ix++; ix == len(cell.Sources) {
			ix = 0
		}
		i, err := os.Open(cell.Sources[ix])
		if err != nil {
			panic(err)
		}
		drawImage(i, cell, updateMu, fb)
		i.Close()
		if cell.RefreshSecs == 0 { // if there is no refreshsecs set then we exit
			wg.Done()
			return
		}
		time.Sleep(time.Second * time.Duration(cell.RefreshSecs))
	}
}

// drawLocalImage goroutine to display an image from the filesystem
func drawLocalImage(wg *sync.WaitGroup, updateMu *sync.Mutex, fb draw.Image, cell CellT) {
	for {
		i, err := os.Open(cell.Source)
		if err != nil {
			panic(err)
		}
		drawImage(i, cell, updateMu, fb)
		i.Close()
		if cell.RefreshSecs == 0 {
			wg.Done()
			return
		}
		time.Sleep(time.Second * time.Duration(cell.RefreshSecs))
	}
}

// drawTime goroutine to display the currnent time using the supplied format
func drawTime(wg *sync.WaitGroup, updateMu *sync.Mutex, fb draw.Image, cell CellT, format string) {
	for {
		timeStr := time.Now().Format(format)
		updateMu.Lock()
		draw.Draw(cell.picture, cell.picture.Bounds(), image.Black, image.ZP, draw.Src)
		writeText(cell.font, cell.FontPts, cell.picture, timeStr)
		draw.Draw(fb, cell.positionRect, cell.picture, image.ZP, draw.Src)
		updateMu.Unlock()
		if cell.RefreshSecs == 0 {
			wg.Done()
			return
		}
		time.Sleep(time.Second * time.Duration(cell.RefreshSecs))
	}
}

// drawURLImage goroutine to display a remote image
func drawURLImage(wg *sync.WaitGroup, updateMu *sync.Mutex, fb draw.Image, cell CellT) {
	for {
		i, err := http.Get(cell.Source)
		if err == nil { // ignore errors here
			drawImage(i.Body, cell, updateMu, fb)
			i.Body.Close()
			if cell.RefreshSecs == 0 {
				wg.Done()
				return
			}
			time.Sleep(time.Second * time.Duration(cell.RefreshSecs))
		}
	}
}

// writeText is a one-shot func to display a short string
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

func (page *PageT) loadConfig(configFilename string) {
	configFile, err := os.Open(configFilename)
	if err != nil {
		panic(err)
	}
	defer configFile.Close()
	configJSON, err := ioutil.ReadAll(configFile)
	if err != nil {
		panic(err)
	}
	err = json.Unmarshal(configJSON, page)
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

// helper funcs

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
