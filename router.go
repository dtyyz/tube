package tube

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"regexp"
	"strings"
	"sync"
)

// map of url params by @name
type Params map[string]string

// route callback data
type Data struct {
	router  *Router
	Writer  http.ResponseWriter // http writer
	Request *http.Request       // http request
	Params  Params              // url params
	status  int
	HTML    string // html response to be parsed
}

// param helper
func (d *Data) P(name string) string {
	return d.Params[name]
}

// write status code
func (d *Data) Status(code int) {
	d.Writer.WriteHeader(code)
	d.status = code
}

// write string response
func (d *Data) Write(str string) {
	io.WriteString(d.Writer, str)
}

// send redirect
func (d *Data) Redirect(url string, code int) {
	http.Redirect(d.Writer, d.Request, url, code)
}

// sends and caches '404 not found'
func (d *Data) NotFound() {
	url := path.Clean(d.Request.URL.Path)
	cacheName := d.Request.Method + " " + url
	d.router.writeCache(cacheName, d.router.route404)
	d.router.callRoute(d.router.route404, url, d.Writer, d.Request)
}

// sends '500 internal server error'
func (d *Data) Error(err error) {
	d.router.logger.Println("internal server error:", err)

	url := path.Clean(d.Request.URL.Path)
	d.router.callRoute(d.router.route500, url, d.Writer, d.Request)
}

// get json request data
func (d *Data) Json(v interface{}) error {
	obj := json.NewDecoder(d.Request.Body)
	obj.DisallowUnknownFields()

	err := obj.Decode(v)
	if err != nil {
		d.Status(http.StatusBadRequest)
		if d.router.logLevel >= LOG_DEBUG {
			d.router.logger.Printf("invalid request %s", err)
		}
		return err
	}

	if obj.More() {
		d.Status(http.StatusBadRequest)
		if d.router.logLevel >= LOG_DEBUG {
			d.router.logger.Println("extra data in request")
		}
		return err
	}

	return nil
}

// write json response
func (d *Data) WriteJson(v interface{}) error {
	b, err := json.Marshal(v)
	if err != nil {
		d.Error(fmt.Errorf("invalid json object %+v", v))
		return err
	}
	d.Write(string(b))
	return nil
}

type Callback func(*Data)

type route struct {
	pattern  *regexp.Regexp
	callback Callback
	params   []string
	method   string
}

const (
	LOG_ERRORS int = iota
	LOG_INFO
	LOG_DEBUG
)

type Router struct {
	routes       []*route
	lateRoutes   []*route
	route404     *route
	route500     *route
	routeCache   map[string]*route
	routeMutex   sync.RWMutex
	htmlCache    map[string]string
	htmlMutex    sync.RWMutex
	noCache      bool
	htmlDisabled bool
	logger       *log.Logger
	logLevel     int
}

func parsePattern(str string) (string, []string) {
	// ensure all routes both begin with a '/' and end without one
	if str != "/" && strings.HasSuffix(str, "/") {
		str = str[:len(str)-1]
	}

	var params []string
	if strings.Contains(str, "@") {
		matches := regexp.MustCompile(`@+(.+?)(?:/|\)|$)`).FindAllStringSubmatch(str, -1)
		for _, match := range matches {
			name := match[1]
			// match wildcard first, then single part
			str = strings.Replace(str, "@@"+name, "(.+)", 1)
			str = strings.Replace(str, "@"+name, "([^/]+)", 1)
			params = append(params, name)
		}
	}

	// ensure pattern can only match the entire url
	str = "^" + str + "$"
	return str, params
}

func (router *Router) createRoute(str string, cb Callback, mthd string, late bool) {
	if router.logLevel >= LOG_DEBUG {
		router.logger.Println("creating route", mthd, str)
	}

	rt := &route{}
	rt.method = mthd

	str, rt.params = parsePattern(str)
	rt.pattern = regexp.MustCompile(str)
	rt.callback = cb

	if late {
		router.lateRoutes = append(router.lateRoutes, rt)
	} else {
		router.routes = append(router.routes, rt)
	}
}

// returns a pattern to match a whole dir (for use with StaticDir)
func (router *Router) Dir(str string) string {
	// do this first so we can put path param after user-defined pattern
	// ensure all routes both begin with a '/' and end without one
	if str != "/" && strings.HasSuffix(str, "/") {
		str = str[:len(str)-1]
	}

	// optional '/' allows both '/assets/test.txt' and '/assets' to match,
	// as most browsers will remove the trailing '/' for urls with no file.
	// this optional '/' is kept out of the parm param for consistency, as it
	// will be added infront of the param for all paths in StaticDir
	str = str + "(?:/?@@path)?"

	return str
}

func (router *Router) Route(method string, str string, cb Callback) {
	router.createRoute(str, cb, method, false)
}

func (router *Router) LateRoute(method string, str string, cb Callback) {
	router.createRoute(str, cb, method, true)
}

func (router *Router) GET(str string, cb Callback) {
	router.createRoute(str, cb, "GET", false)
}

func (router *Router) POST(str string, cb Callback) {
	router.createRoute(str, cb, "POST", false)
}

func (router *Router) PUT(str string, cb Callback) {
	router.createRoute(str, cb, "PUT", false)
}

func (router *Router) DELETE(str string, cb Callback) {
	router.createRoute(str, cb, "DELETE", false)
}

func (router *Router) HEAD(str string, cb Callback) {
	router.createRoute(str, cb, "HEAD", false)
}

func (router *Router) PATCH(str string, cb Callback) {
	router.createRoute(str, cb, "PATCH", false)
}

// returns a Callback function for serving static files from a directory
func (router *Router) StaticDir(dir string) func(*Data) {
	fileServer := http.FileServer(staticFs{http.Dir(dir)})

	return func(d *Data) {
		// overwrite url with relative param path
		_, hasPath := d.Params["path"]
		if hasPath {
			d.Request.URL.Path = "/" + d.Params["path"]
		}

		// determine index.html path for html parser
		// if no file extension, use path/index.html if exists
		if !router.htmlDisabled && path.Ext(d.Request.URL.Path) == "" {
			if _, err := os.Stat(path.Join(dir, d.Request.URL.Path, "index.html")); err == nil {
				d.Request.URL.Path = path.Join(d.Request.URL.Path, "index.html")
			}
		}

		// use html parser if enabled
		if !router.htmlDisabled && strings.HasSuffix(d.Request.URL.Path, ".html") {
			router.serveHTMLStatic(d, dir, d.Request.URL.Path)
		} else {
			if _, err := os.Stat(path.Join(dir, d.Request.URL.Path)); err != nil {
				d.NotFound()
				return
			}
			fileServer.ServeHTTP(d.Writer, d.Request)
		}
	}
}

// returns a Callback function for serving a single static file
func (router *Router) StaticFile(fn string) func(*Data) {
	basefn := path.Base(fn)
	dir := path.Dir(fn)

	return func(d *Data) {
		if !router.htmlDisabled && strings.HasSuffix(fn, ".html") {
			d.Request.URL.Path = basefn
			router.serveHTMLStatic(d, dir, d.Request.URL.Path)
		} else {
			if _, err := os.Stat(fn); err != nil {
				d.Error(fmt.Errorf("static file mapped to nonexistent file"))
				return
			}
			http.ServeFile(d.Writer, d.Request, fn)
		}
	}
}

// set 404 route
func (router *Router) Set404(cb Callback) {
	rt := &route{}
	rt.callback = cb
	router.route404 = rt
}

// set 500 route
func (router *Router) Set500(cb Callback) {
	rt := &route{}
	rt.callback = cb
	router.route500 = rt
}

func (router *Router) callRoute(rt *route, url string, w http.ResponseWriter, r *http.Request) {
	p := Params{}
	if len(rt.params) > 0 {
		matches := rt.pattern.FindAllStringSubmatch(url, -1)
		params := matches[0][1:]
		for num, val := range params {
			p[rt.params[num]] = val
		}
	}

	data := &Data{router, w, r, p, http.StatusOK, ""}
	if rt == router.route404 {
		data.Status(http.StatusNotFound)
	} else if rt == router.route500 {
		data.Status(http.StatusInternalServerError)
	}
	rt.callback(data)

	if data.HTML != "" {
		router.serveHTML(data, false, data.HTML, "/")
	}

	if router.logLevel >= LOG_INFO || data.status == http.StatusInternalServerError {
		router.logger.Println(r.Method, url, data.status)
	}
}

func (router *Router) writeCache(cacheName string, rt *route) {
	router.routeMutex.Lock()
	router.routeCache[cacheName] = rt
	router.routeMutex.Unlock()
}

func (router *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	url := path.Clean(r.URL.Path)
	r.URL.Path = url

	method := r.Method
	cacheName := method + " " + url

	// use cached route
	router.routeMutex.RLock()
	route, cached := router.routeCache[cacheName]
	router.routeMutex.RUnlock()
	if cached && !router.noCache {
		router.callRoute(route, url, w, r)
		return
	}

	// use first route that matches request URL
	for _, rt := range router.routes {
		if rt.method == method && rt.pattern.MatchString(url) {
			router.writeCache(cacheName, rt)
			router.callRoute(rt, url, w, r)
			return
		}
	}

	// check late routes last
	for _, rt := range router.lateRoutes {
		if rt.method == method && rt.pattern.MatchString(url) {
			router.writeCache(cacheName, rt)
			router.callRoute(rt, url, w, r)
			return
		}
	}

	// not found
	router.writeCache(cacheName, router.route404)
	router.callRoute(router.route404, url, w, r)
}

// clear cache that matches a pattern
func (router *Router) ClearCache(str string) {
	str = "[A-Z]+ " + str // METHOD url/foo/bar
	rx, _ := parsePattern(str)
	pattern := regexp.MustCompile(rx)
	router.routeMutex.Lock()
	for url := range router.routeCache {
		if pattern.MatchString(url) {
			delete(router.routeCache, url)
		}
	}
	router.routeMutex.Unlock()

	router.htmlMutex.Lock()
	for url := range router.htmlCache {
		if pattern.MatchString(url) {
			delete(router.htmlCache, url)
		}
	}
	router.htmlMutex.Unlock()
}

// empty cache completely
func (router *Router) EmptyCache() {
	router.routeMutex.Lock()
	router.htmlMutex.Lock()
	clear(router.routeCache)
	clear(router.htmlCache)
	router.htmlMutex.Unlock()
	router.routeMutex.Unlock()
}

// removes routes that patch a pattern
func (router *Router) RemoveRoute(str string) {
	for i, rt := range router.routes {
		if rt.pattern.MatchString(str) {
			router.routes = append(router.routes[:i], router.routes[i+1:]...)
			router.ClearCache(str)
		}
	}
}

// set log level
func (router *Router) SetLogLevel(i int) {
	router.logLevel = i
}

// set logger
func (router *Router) SetLogger(logger *log.Logger) {
	router.logger = logger
}

// create and initialize new router
func NewRouter() *Router {
	router := &Router{}
	router.routeCache = map[string]*route{}
	router.htmlCache = map[string]string{}

	// default error routes
	rt404 := &route{}
	rt404.callback = func(d *Data) {
		io.WriteString(d.Writer, "404 file not found")
	}
	router.route404 = rt404

	rt500 := &route{}
	rt500.callback = func(d *Data) {
		io.WriteString(d.Writer, "500 internal server error")
	}
	router.route500 = rt500

	if os.Getenv("NOCACHE") == "1" {
		router.noCache = true
	}

	if os.Getenv("NOHTML") == "1" {
		router.htmlDisabled = true
	}

	router.logger = log.New(os.Stderr, "tube: ", log.LstdFlags|log.Lmsgprefix)
	router.logger.Println("router initialized")

	return router
}
