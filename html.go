package tube

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"regexp"
	"strings"
)

func readHTMLFile(fn string) (string, error) {
	// remove prefixing '/' from filename for consistency as paths
	// in include tags will likely not have them (but URLs will)
	fn = strings.TrimPrefix(fn, "/")

	// read file
	b, err := os.ReadFile(fn)
	if err != nil {
		return "", err
	}
	str := string(b)

	return str, nil
}

func (router *Router) parseHTML(str string, parentDir string) (string, error) {
	// find and parse include tags
	includes := regexp.MustCompile(`<!--\s*include "(.+?\.html)"\s*-->`).FindAllStringSubmatch(str, -1)
	if len(includes) > 0 {
		for _, match := range includes {
			tag := match[0]
			include := match[1]

			// read referenced file
			fn := path.Join(parentDir, include)
			body, err := readHTMLFile(fn)
			if err != nil {
				return "", fmt.Errorf("unreadable include %s", err)
			}

			// parse it
			nextDir := path.Join(parentDir, path.Dir(include))
			contents, err := router.parseHTML(body, nextDir)
			if err != nil {
				return "", err
			}

			// replace tag with file contents
			str = strings.Replace(str, tag, contents, 1)
		}
	}

	// find and parse conditional tags
	conditionals := regexp.MustCompile(`<!--\s*if (!)?\$(.+?)\s+{\s*(.+)\s*}\s*-->`).FindAllStringSubmatch(str, -1)
	if len(conditionals) > 0 {
		for _, match := range conditionals {
			tag := match[0]
			invchar := match[1]
			varname := match[2]
			text := match[3]

			invert := invchar == "!"
			env := os.Getenv(varname)
			hasEnv := env != ""

			contents := ""
			if (!invert && hasEnv) || (invert && !hasEnv) {
				contents = text
			}

			str = strings.Replace(str, tag, contents, 1)

		}
	}

	return str, nil
}

func (router *Router) serveHTMLStatic(d *Data, dir string, fn string) {
	fn = path.Join(dir, fn)
	body, err := readHTMLFile(fn)
	if err != nil {
		d.Error(fmt.Errorf("unreadable static include %s", err))
		return
	}
	router.serveHTML(d, true, body, dir)
}

func (router *Router) serveHTML(d *Data, static bool, text string, dir string) {
	url := d.Request.URL.Path

	// serve from cache if exists
	if static {
		router.htmlMutex.RLock()
		html, cached := router.htmlCache[url]
		router.htmlMutex.RUnlock()
		if cached && !router.noCache {
			io.WriteString(d.Writer, html)
			return
		}
	}

	// parse and serve
	str, err := router.parseHTML(text, dir)

	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			d.NotFound()
			return
		} else {
			d.Error(err)
			return
		}
	}

	io.WriteString(d.Writer, str)

	// save to cache
	if static {
		router.htmlMutex.Lock()
		router.htmlCache[url] = str
		router.htmlMutex.Unlock()

	}
}
