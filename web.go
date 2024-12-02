package zns

import (
	"embed"
	"io/fs"
)

//go:embed web
var webStatic embed.FS

var Static fs.FS

func init() {
	var err error
	Static, err = fs.Sub(webStatic, "web")
	if err != nil {
		panic(err)
	}
}
