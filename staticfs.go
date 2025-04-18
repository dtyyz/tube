package tube

import (
	"net/http"
	"os"
	"path"
)

// filesystem override for http.FileServer (not used for HTML parser)
//	- ensures directory listings are fully disabled
//	- reliably serves index.html for directories

type staticFsFile struct {
	http.File
}

func (f staticFsFile) Readdir(count int) ([]os.FileInfo, error) {
	return nil, nil
}

type staticFs struct {
	base http.FileSystem
}

func (fs staticFs) Open(name string) (http.File, error) {
	f, err := fs.base.Open(name)
	if err != nil {
		return nil, err
	}

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}

	// handle dirs
	if info.IsDir() {
		// serve index if exists
		index, err := fs.base.Open(path.Join(name, "index.html"))
		if err == nil {
			// allow http.FileServer to serve index for root
			// as a weird redirect loop will result otherwise.
			// still show 404 if index doesn't exist
			if name != "/" {
				return staticFsFile{index}, nil
			}
		} else {
			return nil, os.ErrNotExist
		}
	}

	// return file with overridden Readdir
	return staticFsFile{f}, nil
}
