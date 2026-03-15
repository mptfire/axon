package attachment

import (
	"errors"
	"fmt"
	"io"
	"regexp"

	"heckel.io/ntfy/v2/model"
	"heckel.io/ntfy/v2/util"
)

// Store is an interface for storing and retrieving attachment files
type Store interface {
	Write(id string, in io.Reader, limiters ...util.Limiter) (int64, error)
	Read(id string) (io.ReadCloser, int64, error)
	Remove(ids ...string) error
	Size() int64
	Remaining() int64
}

var (
	fileIDRegex      = regexp.MustCompile(fmt.Sprintf(`^[-_A-Za-z0-9]{%d}$`, model.MessageIDLength))
	errInvalidFileID = errors.New("invalid file ID")
)
