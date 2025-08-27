package linux

import (
	"embed"
)

//go:embed *
var StaticBinaries embed.FS
