package sprig

import "errors"

func fail(msg string) (string, error) {
	return "", errors.New(msg)
}
