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
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/disintegration/imaging"
	"github.com/golang/freetype"
	"github.com/golang/freetype/truetype"
)

const fontFile = "LeagueMono-Regular.ttf"

// PageT describes the contents of a fbinfogrid page (display)
type PageT struct {
	Name       string
	Rows, Cols int
	Cells      []CellT
}

// CellT describes a piece of information on a page
type CellT struct {
	Row, Col         int
	Rowspan, Colspan int
	RefreshSecs      int
	CellType         string
	Source, Text     string
	FontPts          float64
	// private fields used in code...
	font         *truetype.Font
	positionRect image.Rectangle
	picture      *image.NRGBA // .RGBA
}

// program arguments
var (
	configFlag = flag.String("config", "config.json", "JSON file describing the information layout")
	fbdevFlag  = flag.String("fbdev", "/dev/fb0", "framebuffer device file")
)

func main() {
	flag.Parse()

	fb, err := Open(*fbdevFlag)
	if err != nil {
		panic(err)
	}
	//defer fb.Close()
	bounds := fb.Bounds()

	var updateMu sync.Mutex
	var wg sync.WaitGroup

	var page PageT
	page.loadConfig(*configFlag)
	fmt.Printf("Definition: for page %s is %v\n", page.Name, page)

	cellWidth := bounds.Dx() / page.Cols
	cellHeight := bounds.Dy() / page.Rows
	fmt.Printf("Page size in pixels is  %d x %d (w x h)\n", bounds.Dx(), bounds.Dy())
	fmt.Printf("Calculated cell size is %d x %d (w x h)\n", cellWidth, cellHeight)
	cellSize := image.Rect(0, 0, cellWidth, cellHeight)

	bg := image.Black
	draw.Draw(fb, bounds, bg, image.ZP, draw.Src)

	font := loadFont(fontFile)

	//for {
	for _, cell := range page.Cells {
		topLeftX := (cell.Col - 1) * cellWidth
		topLeftY := (cell.Row - 1) * cellHeight

		cell.positionRect = image.Rect(topLeftX, topLeftY, topLeftX+cellWidth, topLeftY+cellHeight) // where it will be drawn
		cell.picture = image.NewNRGBA(cellSize)
		cell.font = font

		switch cell.CellType {
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
		case "localimage":
			wg.Add(1)
			go drawLocalImage(&wg, &updateMu, fb, cell)
		case "text":
			if cell.FontPts == 0.0 {
				cell.FontPts = 80.0
			}
			updateMu.Lock()
			writeText(font, cell.FontPts, cell.picture, 0, cell.picture.Bounds().Dy()/2, cell.Text)
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

func drawLocalImage(wg *sync.WaitGroup, updateMu *sync.Mutex, fb draw.Image, cell CellT) {
	for {
		i, err := os.Open(cell.Source)
		if err != nil {
			panic(err)
		}
		defer i.Close()
		sImg, _, err := image.Decode(i)
		sImg = imaging.Resize(sImg, cell.picture.Bounds().Dx(), 0, imaging.NearestNeighbor)
		if err != nil {
			panic(err)
		}
		updateMu.Lock()
		draw.Draw(fb, cell.positionRect, sImg, image.ZP, draw.Src)
		updateMu.Unlock()
		if cell.RefreshSecs == 0 {
			wg.Done()
			return
		}
		time.Sleep(time.Second * time.Duration(cell.RefreshSecs))
	}
}

func drawTime(wg *sync.WaitGroup, updateMu *sync.Mutex, fb draw.Image, cell CellT, format string) {
	for {
		timeStr := time.Now().Format(format)
		updateMu.Lock()
		draw.Draw(cell.picture, cell.picture.Bounds(), image.Black, image.ZP, draw.Src)
		writeText(cell.font, cell.FontPts, cell.picture, 0, cell.picture.Bounds().Dy()/2, timeStr)
		draw.Draw(fb, cell.positionRect, cell.picture, image.ZP, draw.Src)
		updateMu.Unlock()
		if cell.RefreshSecs == 0 {
			wg.Done()
			return
		}
		time.Sleep(time.Second * time.Duration(cell.RefreshSecs))
	}
}

func drawURLImage(wg *sync.WaitGroup, updateMu *sync.Mutex, fb draw.Image, cell CellT) {
	for {
		i, err := http.Get(cell.Source)
		if err == nil { // ignore errors here
			defer i.Body.Close()
			sImg, _, err := image.Decode(i.Body)
			if err != nil {
				panic(err)
			}
			sImg = imaging.Resize(sImg, cell.picture.Bounds().Dx(), 0, imaging.NearestNeighbor)
			updateMu.Lock()
			draw.Draw(fb, cell.positionRect, sImg, image.ZP, draw.Src)
			updateMu.Unlock()
			if cell.RefreshSecs == 0 {
				wg.Done()
				return
			}
			time.Sleep(time.Second * time.Duration(cell.RefreshSecs))
		}
	}
}

func writeText(font *truetype.Font, pts float64, img draw.Image, x, y int, text string) {
	c := freetype.NewContext()
	c.SetFont(font)
	c.SetFontSize(pts)
	c.SetClip(img.Bounds())
	c.SetDst(img)
	c.SetSrc(image.White)
	pt := freetype.Pt(x, y)
	c.DrawString(text, pt)
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
		log.Println(err)
		return nil
	}
	font, err := freetype.ParseFont(fontBytes)
	if err != nil {
		log.Println(err)
		return nil
	}
	return font
}
