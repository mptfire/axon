package server

// axon: embed the IANA timezone database so digest scheduling works in minimal
// containers (e.g. alpine, distroless) that ship without /usr/share/zoneinfo.
// Costs ~450 KB in the binary; only imported for its side effect.
import _ "time/tzdata"
