package main

import (
	"config"

	"github.com/alecthomas/kong"
)

var global_profile config.Profile

var CLI struct {
	Profile struct {
		Add     AddProfileCmd     `cmd`
		Delete  DeleteProfileCmd  `cmd`
		List    ListProfileCmd    `cmd`
		Default DefaultProfileCmd `cmd`
	} `cmd`
	Query QueryCmd `cmd`
}

func main() {
	ctx := kong.Parse(&CLI)
	config, e := config.ReadConfigFromFile("config.toml")
	if e == nil {
		profile := config.Profiles[config.Default_profile]
		global_profile = profile
	}
	err := ctx.Run(ctx)
	ctx.FatalIfErrorf(err)
}
