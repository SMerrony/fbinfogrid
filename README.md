# fbinfogrid
Display a configurable grid of information and images on the Raspberry Pi framebuffer

## Usage
Type ```./fbinfogrid -h``` for help.  

If you do not have a ```config.json``` file in the working directory then you must use the ```-config``` option 
to specify a grid configuration file.

If any cell has "refreshsecs" defined to be > 0 then the program will not exit until it is killed, 
otherwise the program will end once the grid has been drawn.

## Configuration
See the included demoX.json files for configuration examples.

Every cell must have ```row```, ```col```, and ```celltype``` specified.

Currently defined information cell types and associated attributes are...

|   Type      |  Description                   | fontpts | refreshsecs | source | text |
|-------------|--------------------------------| :-----: | :---------: | :----: | :--: |
| carousel    | Slideshow of images            |    N    |      Y*     |    **  |   N  |
| datemonth   | eg. "2 Jan"                    |    Y    |      Y      |    N   |   N  |
| day         | eg. "Mon"                      |    Y    |      Y      |    N   |   N  |
| localimage  | An image stored locally        |    N    |      Y      |    Y*  |   N  |
| text        | Text that is never updated     |    Y    |      N      |    N   |   Y* |
| time        | eg. "15:04"                    |    Y    |      Y      |    N   |   N  |
| urlimage    | An image (JPEG/PNG) from a URL |    N    |      Y      |    Y*  |   N  |

(* these attributes _must_ be specified)

(** must specify a ```sources``` array - see [demo03.json](demo03.json))  
