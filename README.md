# fbinfogrid
Display a configurable grid of information and images on the Raspberry Pi framebuffer.

## Usage
Type ```./fbinfogrid -h``` for help.  

If you do not have a ```config.json``` file in the working directory then you must use the ```-config``` option 
to specify a grid configuration file.

If any cell has "refreshsecs" defined to be > 0 then the program will not exit until it is killed, 
otherwise the program will end once the grid has been drawn.

## Configuration
See the included demoX.json files for configuration examples.

The configuration describes a page of cells.  

### Page
Page attributes are...

| Type | Compulsory | Description |
|------| :--------: |-------------|
| name |     N      | Page description |
| rows |     Y      | No. of rows |
| cols |     Y      | No. of columns |
| fontfile | N      | Path of a TTF font, defaults to supplied LeagueMono-Regular.ttf |

### Cells

Every cell **must** have ```row```, ```col```, and ```celltype``` specified.

You **may** also specify ```rowspan``` and ```colspan``` for any cell;
see [demo04.json](demo04.json) for a an example.
Note that the behaviour of overlapping cells is currently undefined.

Currently defined information cell types and associated attributes are...

|   Type      |  Description                   | fontpts | refreshsecs | source | text |
|-------------|--------------------------------| :-----: | :---------: | :----: | :--: |
| carousel    | Slideshow of images            |    N    |      Y*     |    **  |   N  |
| datemonth   | eg. "2 Jan"                    |    Y    |      Y      |    N   |   N  |
| day         | eg. "Mon"                      |    Y    |      Y      |    N   |   N  |
| daydatemonth | eg. "Mon 2 Jan"               |    Y    |      Y      |    N   |   N  |
| hostname    | eg. "raspipi01"                |    Y    |      N      |    N   |   N  |
| localimage  | An image stored locally        |    N    |      Y      |    Y*  |   N  |
| text        | Text that is never updated     |    Y    |      N      |    N   |   Y* |
| time        | eg. "15:04"                    |    Y    |      Y      |    N   |   N  |
| urlimage    | An image (JPEG/PNG) from a URL |    N    |      Y      |    Y*  |   N  |

(* these attributes **must** be specified)

(** **must** specify a ```sources``` array - see [demo03.json](demo03.json))  

Image cells that refresh (i.e. have a non-zero ```refreshsecs```) reload the image on each refresh, 
so if the underlying file changes that change will appear on the next refresh.

Images are scaled to fill the cell whilst maintaining their original aspect ratio.
This will result in a certain amount of cropping if the image and cell proportions differ.
