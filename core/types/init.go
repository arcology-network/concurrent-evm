package types

import (
	"encoding/gob"
)

func init() {
	gob.Register(&Receipt{})
	gob.Register([]*Receipt{})
}
