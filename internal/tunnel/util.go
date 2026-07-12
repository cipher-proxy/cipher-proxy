package tunnel

import (
	"errors"
	"strconv"
)

func itoa(n int) string {
	return strconv.Itoa(n)
}

var errTunnelDown = errors.New("ssh tunnel not connected")
