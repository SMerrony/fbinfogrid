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

// Since late 2015 the default framebuffer depth under Raspbian is 32 bits, this yields the best performance.
// Append vt.global_cursor_default=0 to /boot/cmdline.txt to disable the blinking cursor.

package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	_ "image/png"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	framebuffer "github.com/gilphilbert/go-framebuffer"

	"github.com/disintegration/imaging"
	"github.com/golang/freetype"
	"github.com/golang/freetype/truetype"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

const (
	defaultConfig      = "config.json"
	defaultFont        = "LeagueMono-Regular.ttf"
	defaultFramebuffer = "fb0"
)

// N.B. In the following 3 types the exported fields may be unmarshalled from the JSON
//      configuration file, non-exported fields are for internal use only.

// ConfigT holds an fbinfogrid configuration (one or more Pages)
type ConfigT struct {
	Pages         []PageT
	currentPageIx int
}

// PageT describes the contents of a fbinfogrid page (display)
type PageT *struct {
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
	Scaling          string
	fn               func(*sync.WaitGroup, *sync.Mutex, CellT)
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
	httpFlag   = flag.Int("http", 0, "port to serve HTTP copy of framebuffer")
)

var (
	fb       *framebuffer.Framebuffer
	fbcopyMu sync.RWMutex
	fbcopy   *image.NRGBA
)

func main() {
	var err error
	flag.Parse()

	fb, err = framebuffer.Open(*fbdevFlag)
	if err != nil {
		panic(err)
	}
	log.Printf("INFO: Page size in pixels is: %d x %d (w x h)\n", fb.Xres, fb.Yres)

	var (
		updateMu sync.Mutex
		wg       sync.WaitGroup
		config   *ConfigT
		stoppers []chan bool
	)

	if *httpFlag != 0 {
		fbcopy = image.NewNRGBA(image.Rect(0, 0, fb.Xres, fb.Yres))
		go httpServer(*httpFlag)
	}

	config = loadConfig(*configFlag)

	blanker := image.NewNRGBA(image.Rect(0, 0, fb.Xres, fb.Yres))
	draw.Draw(blanker, blanker.Bounds(), image.Black, image.ZP, draw.Src)

	config.currentPageIx = -1
	for {
		if config.currentPageIx++; config.currentPageIx == len(config.Pages) {
			config.currentPageIx = 0
		}
		page := config.Pages[config.currentPageIx]

		if page.FontFile == "" {
			page.FontFile = defaultFont
		}

		page.cellWidth = fb.Xres / page.Cols
		page.cellHeight = fb.Yres / page.Rows
		// fmt.Printf("Calculated cell size is: %d x %d (w x h)\n", page.cellWidth, page.cellHeight)

		render(image.Rect(0, 0, fb.Xres, fb.Yres), blanker)
		page.font = loadFont(page.FontFile)

		for _, cell := range page.Cells {
			prepareCell(page, cell)
			stopper := startOrExecute(&wg, &updateMu, cell)
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
	// fmt.Printf("Cell prepared at %v\n", cell.positionRect)
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

	default:
		log.Fatalf("ERROR: Unknown cell type %s\n", cell.CellType)
	}
}

// funcs for handling each cell type

// drawCarousel goroutine to show rotating selection of images indefinitely
func drawCarousel(wg *sync.WaitGroup, updateMu *sync.Mutex, cell CellT) {
	if cell.currentSrcIx++; cell.currentSrcIx == len(cell.Sources) {
		cell.currentSrcIx = 0
	}
	i, err := os.Open(cell.Sources[cell.currentSrcIx])
	if err != nil {
		panic(err)
	}
	drawImage(i, cell, updateMu)
	i.Close()
}

// drawIsAlive displays an indicator that a host is accessible
func drawIsAlive(wg *sync.WaitGroup, updateMu *sync.Mutex, cell CellT) {
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
	render(cell.positionRect, cell.picture)
	updateMu.Unlock()
}

// drawLocalImage displays an image from the filesystem
func drawLocalImage(wg *sync.WaitGroup, updateMu *sync.Mutex, cell CellT) {
	i, err := os.Open(cell.Source)
	if err != nil {
		panic(err)
	}
	drawImage(i, cell, updateMu)
	i.Close()
}

//drawText displays the cell's current text
func drawText(wg *sync.WaitGroup, updateMu *sync.Mutex, cell CellT) {
	updateMu.Lock()
	writeText(cell.font, cell.FontPts, cell.picture, cell.Text)
	render(cell.positionRect, cell.picture)
	updateMu.Unlock()
}

// drawTime displays the currnent time using the supplied format
func drawTime(wg *sync.WaitGroup, updateMu *sync.Mutex, cell CellT) {
	timeStr := time.Now().Format(cell.format)
	updateMu.Lock()
	draw.Draw(cell.picture, cell.picture.Bounds(), image.Black, image.ZP, draw.Src)
	writeText(cell.font, cell.FontPts, cell.picture, timeStr)
	render(cell.positionRect, cell.picture)
	updateMu.Unlock()
}

// drawURLImage displays a remote image
func drawURLImage(wg *sync.WaitGroup, updateMu *sync.Mutex, cell CellT) {
	i, err := http.Get(cell.Source)
	if err == nil { // ignore errors here
		drawImage(i.Body, cell, updateMu)
		i.Body.Close()
	}
}

// helper funcs

// drawImage copies the cell's image into the framebuffer
func drawImage(img io.Reader, cell CellT, updateMu *sync.Mutex) {
	sImg, _, err := image.Decode(img)
	if err != nil {
		log.Printf("WARNING: Could not render image due to %s", err)
		return
	}
	w := cell.picture.Bounds().Dx()
	h := cell.picture.Bounds().Dy()
	switch cell.Scaling {
	case "fit":
		sImg = imaging.Fit(sImg, w, h, imaging.NearestNeighbor)
	case "fill":
		sImg = imaging.Fill(sImg, w, h, imaging.Center, imaging.NearestNeighbor)
	default:
		sImg = imaging.Resize(sImg, w, h, imaging.NearestNeighbor)
	}
	updateMu.Lock()
	render(cell.positionRect, sImg)
	updateMu.Unlock()
}

func httpServer(port int) {
	http.HandleFunc("/", fbcopyHandler)
	err := http.ListenAndServe(":"+strconv.Itoa(port), nil)
	if err != nil {
		panic(err)
	}
}

func fbcopyHandler(w http.ResponseWriter, req *http.Request) {
	// NoCompression is actually faster than BestSpeed, but the resultant image
	// is typically much larger resulting in longer transmission times...
	enc := &png.Encoder{CompressionLevel: png.BestSpeed}
	// enc := &png.Encoder{CompressionLevel: png.NoCompression}
	buff := new(bytes.Buffer)
	fbcopyMu.RLock()
	enc.Encode(buff, fbcopy)
	fbcopyMu.RUnlock()
	w.Header().Set("Refresh", "60") // the browser will reload the image every 60 seconds
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Content-Length", strconv.Itoa(len(buff.Bytes())))
	w.Write(buff.Bytes())
}

func loadConfig(configFilename string) (config *ConfigT) {
	configFile, err := os.Open(configFilename)
	if err != nil {
		panic(err)
	}
	defer configFile.Close()
	configJSON, err := ioutil.ReadAll(configFile)
	if err != nil {
		panic(err)
	}
	var newConf ConfigT
	err = json.Unmarshal(configJSON, &newConf)
	if err != nil {
		panic(err)
	}
	return &newConf
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

func render(destRect image.Rectangle, srcImg image.Image) {
	fb.DrawImage(destRect.Min.X, destRect.Min.Y, srcImg)
	if fbcopy != nil {
		fbcopyMu.Lock()
		draw.Draw(fbcopy, destRect, srcImg, image.Point{0, 0}, draw.Src)
		fbcopyMu.Unlock()
	}
}

func startOrExecute(wg *sync.WaitGroup, updateMu *sync.Mutex, cell CellT) (stop chan bool) {
	if cell.RefreshSecs == 0 {
		// one-shot execute
		cell.fn(wg, updateMu, cell)
		return nil
	}
	// regular execution
	cell.fn(wg, updateMu, cell)
	ticker := time.NewTicker(time.Second * time.Duration(cell.RefreshSecs))
	stop = make(chan bool)
	go func() { //wg *sync.WaitGroup, updateMu *sync.Mutex, fb *framebuffer.Framebuffer) { //}, cell CellT) {
		for {
			select {
			case <-stop:
				wg.Done()
				return
			case <-ticker.C:
				cell.fn(wg, updateMu, cell)
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
