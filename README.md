# fbinfogrid
Display a configurable grid of information and images on a Raspberry Pi (or other Linux) framebuffer.

Clock, image carousel, and updating webcam snapshots...
![fbinfogrid screenshot 1](screenshots/demo02.png) 

Simple network device monitoring...
![fbinfogrid network monitoring](screenshots/hostmon1.png) 

The information may optionally be made available via HTTP.

## Usage
Type ```./fbinfogrid -h``` for help.  

You may supply a ```config.json``` file in the working directory or you can use the ```-config``` option 
to specify a grid configuration file.

If any cell has "refreshsecs" defined to be > 0 then the program will not exit until it is killed, 
otherwise the program will end once the grid has been drawn.

## Configuration
See the included JSON files in the [configs](configs) folder for configuration examples.

A configuration describes page(s) of cells.  

### Page
Page attributes are...

| Type     | Compulsory | Description |
|----------| :--------: |-------------|
| name     |     N      | Page description, not displayed |
| rows     |     Y      | No. of rows |
| cols     |     Y      | No. of columns |
| fontfile |     N      | Path of a TTF font, defaults to supplied LeagueMono-Regular.ttf |
| durationmins | N      | How many minutes to wait before moving to the next page (no default) |

See [demoTwoPages.json](configs/demoTwoPages.json) for a multiple-page example.

### Cells

Every cell **must** have ```row```, ```col```, and ```celltype``` specified.

You **may** also specify ```rowspan``` and ```colspan``` for any cell;
see [demoSpans.json](configs/demoSpans.json) for an example.
Note that the behaviour of overlapping cells is currently undefined.

Currently defined information cell types and associated attributes are...

|   Type      |  Description                   | fontpts | refreshsecs | scaling | source | text |
|-------------|--------------------------------| :-----: | :---------: | :-----: | :----: | :--: |
| carousel    | Slideshow of images            |    N    |      Y*     |    Y    |    **  |   N  |
| datemonth   | eg. "2 Jan"                    |    Y    |      Y      |    N    |    N   |   N  |
| day         | eg. "Mon"                      |    Y    |      Y      |    N    |    N   |   N  |
| daydatemonth | eg. "Mon 2 Jan"               |    Y    |      Y      |    N    |    N   |   N  |
| hostname    | eg. "raspipi01"                |    Y    |      N      |    N    |    N   |   N  |
| isalive     | Is a host reachable via TCP?   |    Y    |      Y*     |    N    |    Y*  |   Y  |
| localimage  | An image stored locally        |    N    |      Y      |    Y    |    Y*  |   N  |
| text        | Text that is never updated     |    Y    |      N      |    N    |    N   |   Y* |
| time        | eg. "15:04"                    |    Y    |      Y      |    N    |    N   |   N  |
| urlimage    | An image (JPEG/PNG) from a URL |    N    |      Y      |    Y    |    Y*  |   N  |

(* these attributes **must** be specified)

(** **must** specify a ```sources``` array - see [demoCarousel.json](configs/demoCarousel.json))  

Image cells that refresh (i.e. have a non-zero ```refreshsecs```) reload the image on each refresh, 
so if the underlying file changes that change will appear on the next refresh.

Scaling may be one of "fill", "fit", or "resize" (default).  Fill and fit maintain the aspect
ratio of the image, so there may be some cropping or borders apparent; resize scales the image to exactly 
fit the cell, so there may be some distortion.
