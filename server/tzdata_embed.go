package server

// axon: embed the IANA timezone database so digest scheduling works in minimal
// containers (e.g. alpine, distroless) that ship without /usr/share/zoneinfo.
// Costs ~450 KB in the binary; imported for its side effect only.
import (
	_ "time/tzdata" // Embed the tz database; required by time.LoadLocation in minimal containers
)
