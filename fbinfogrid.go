// Must do  this first: sudo fbset -fb /dev/fb0 -depth 16
// unless 16-bit framebuffer depth is set in config.txt

package main

import (
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg"
	_ "image/png"
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/disintegration/imaging"
	"github.com/golang/freetype"
	"github.com/golang/freetype/truetype"
	"github.com/gonutz/framebuffer"
)

const (
	width, height = 600, 600
	//centre        = width / 2.0
	degreesIncr = 0.1 * math.Pi / 180
	turns       = 2
	stop        = 360 * turns * 10 * degreesIncr
)

const (
	fbFileName     = "/dev/fb0"
	fontFile       = "LeagueMono-Regular.ttf"
	configFileName = "page.json"
)

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
	// private fields used in code...
	font         *truetype.Font
	fontPts      float64
	positionRect image.Rectangle
	picture      *image.RGBA
}

func main() {
	fb, err := framebuffer.Open(fbFileName)
	if err != nil {
		panic(err)
	}
	defer fb.Close()
	bounds := fb.Bounds()

	var updateMu sync.Mutex
	var wg sync.WaitGroup

	var page PageT
	page.loadConfig(configFileName)
	fmt.Printf("Definition: for page %s is %v\n", page.Name, page)

	cellWidth := bounds.Dx() / page.Cols
	cellHeight := bounds.Dy() / page.Rows
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
		cell.picture = image.NewRGBA(cellSize)
		cell.font = font

		switch cell.CellType {
		case "datemonth":
			cell.fontPts = 80.0
			wg.Add(1)
			go drawTime(&wg, &updateMu, fb, cell, "2 Jan")
		case "day":
			cell.fontPts = 80.0
			wg.Add(1)
			go drawTime(&wg, &updateMu, fb, cell, "Mon")
		case "staticimage":
			i, err := os.Open(cell.Source)
			if err != nil {
				panic(err)
			}
			defer i.Close()
			sImg, _, err := image.Decode(i)
			sImg = imaging.Resize(sImg, cellWidth, 0, imaging.NearestNeighbor)
			if err != nil {
				panic(err)
			}
			updateMu.Lock()
			draw.Draw(fb, cell.positionRect, sImg, image.ZP, draw.Src)
			updateMu.Unlock()
		case "text":
			updateMu.Lock()
			writeText(font, 96.0, cell.picture, 0, cell.picture.Bounds().Dy()/2, cell.Text)
			draw.Draw(fb, cell.positionRect, cell.picture, image.ZP, draw.Src)
			updateMu.Unlock()
		case "time":
			cell.fontPts = 128.0
			wg.Add(1)
			go drawTime(&wg, &updateMu, fb, cell, "15:04")
		case "urlimage":
			i, err := http.Get(cell.Source)
			if err == nil { // ignore errors here
				defer i.Body.Close()
				sImg, _, err := image.Decode(i.Body)
				if err != nil {

					panic(err)
				}
				sImg = imaging.Resize(sImg, cellWidth, 0, imaging.NearestNeighbor)
				//draw.Draw(txt, cell.positionRect, sImg, image.ZP, draw.Src)
				updateMu.Lock()
				draw.Draw(fb, cell.positionRect, sImg, image.ZP, draw.Src)
				updateMu.Unlock()
			}
		}
	}

	wg.Wait()

	//}
	//black := image.NewUniform(color.Black)
	//draw.Draw(fb, bounds, black, image.ZP, draw.Src)
}

func drawTime(wg *sync.WaitGroup, updateMu *sync.Mutex, fb *framebuffer.Device, cell CellT, format string) {
	for {
		timeStr := time.Now().Format(format)
		updateMu.Lock()
		draw.Draw(cell.picture, cell.picture.Bounds(), image.Black, image.ZP, draw.Src)
		writeText(cell.font, cell.fontPts, cell.picture, 0, cell.picture.Bounds().Dy()/2, timeStr)
		draw.Draw(fb, cell.positionRect, cell.picture, image.ZP, draw.Src)
		updateMu.Unlock()
		if cell.RefreshSecs == 0 {
			wg.Done()
			return
		}
		time.Sleep(time.Second * time.Duration(cell.RefreshSecs))
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

func (page *PageT) loadConfig(confFile string) {
	configFile, err := os.Open(configFileName)
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
