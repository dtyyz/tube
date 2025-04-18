# tube
### yet another go router  
the internet is a series of **tube**s  
this is just one of them

## features
 - regex routes
 - named url parameters
 - html include tags

## usage
```go
package main
import (
	"fmt"
	"io"
	"net/http"
	"github.com/dtyyz/tube"
)
// create router
var router *tube.Router = tube.NewRouter()
func main() {
	// create a GET route
	router.GET("/test/@foo/@bar", testRoute)
	// listen with standard go net/http server
	http.ListenAndServe("localhost:8888", router)
}
// http://localhost:8888/test/abc/123
func testRoute(d *tube.Data) {
	// send a response
	d.Write("hello browser\n")
	// access url params by name
	fmt.Println(d.Params["foo"], d.Params["bar"])
}
```

## static files
static files can be served in a couple ways...
```go
// ...as a single file
router.GET("/", router.StaticFile("./static/index.html"))
// ...as a directory
router.GET(router.Dir("/assets"), router.StaticDir("./static"))
```

## late routes
late routes will match after all other routes so that you can
use them as a wildcard without having to worry about the order
in which routes are defined.
```go
// create a late route
router.LateRoute("GET", router.Dir("/assets"), router.StaticDir("./static"))
```

## custom error routes
you can define your own 404 and 500 routes a couple ways...
```go
// ...as a route callback
router.Set404(func(d *tube.Data) {
	d.Write("couldn't find that!\n")
})
// ...as a static file
router.Set404(router.StaticFile("./static/404.html"))
```

## authentication
conditionally serving routes can be achieved by wrapping your callback...
```go
func AuthWrapper(cb tube.Callback) tube.Callback {
	return func(d *tube.Data) {
		someCondition := false
		if someCondition {
			cb(d)
		} else {
			d.Status(http.StatusUnauthorized)
			d.Write("unauthorized")
		}
	}
}
router.GET(router.Dir("/admin"), AuthWrapper(router.StaticDir("./static")))
```

## html preprocessing
html files are preprocessed to parse include tags and more  
this behaviour can be disabled with the env `NOHTML=1`
```go
// you can also return html from a route callback
func myRouteCallback(d *tube.data) {
	d.HTML += `<!-- include "test.html" -->`
}
```

```html
// replaced with the contents of foobar.html
// can be nested as much as needed (don't loop!)
// paths are relative to the current file
<!-- include "subdir/foobar.html" -->

// replaced with the text inside { } if DEV=1 is in env
<!-- if $DEV { <script src="dev.js"></script> } -->

// can be negated with ! for when env isn't present or == 0
<!-- if !$DEV { <script src="production.js"></script> } -->
```

## disable caching
route and html caching can be disabled for debug with the env `NOCACHE=1`

## deleting routes
```go
// remove route completely
router.RemoveRoute("/users/@name")
// clear cache if routes are updated at runtime
router.ClearCache("/users/doug")
```
