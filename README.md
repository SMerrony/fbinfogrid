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

Currently defined information cell types are...

|   Type      |  Description                   |  Usage  |
|-------------|--------------------------------|---------|
| datemonth   | eg. "2 Jan"                    | May set "refreshsecs" to be eg. 1800 |
| day         | eg. "Mon"                      | May set "refreshsecs" to be eg. 1800 |
| staticimage | An image that is never updated | Must specify local path as "source" |
| text        | Text that is never updated     | Must specify a string as "text"  |
| time        | eg. "15:04"                    | May set "refreshsecs" to be eg. 30 |
| urlimage    | An image (JPEG/PNG) from a URL | Must set "source" as the direct URL |

