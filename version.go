package main

import "github.com/romaine-life/fzt/render"

// version is the release the suite stamps into main (pipeline-templates' release suite);
// fzt-automate reports it as render.Version, and a local build as "dev".
var version string

func init() {
	render.Version = version
	if version == "" {
		render.Version = "dev"
	}
}
