package main

import (
	"encoding/json"
	"flag"
	"io/ioutil"
	"log"

	"github.com/matthewmcnew/package-extractor/buildpackage"
)

var (
	source         = flag.String("from", "", "location to extract from")
	destination    = flag.String("to", "", "builpackage location to write to")
	tarDestination = flag.String("to-file", "", "file location to write to")
	id             = flag.String("id", "", "id to extract")
	version        = flag.String("version", "", "version to extract")
	builderAll     = flag.String("builder-all", "", "extract all buildpacks from builder")
	results        = flag.String("results", "", "path to write results")
)

func main() {
	flag.Parse()

	if *builderAll != "" {
		r, err := buildpackage.ExtractAll(*source, *destination)
		if err != nil {
			log.Fatal(err)
		}

		if *results != "" {
			file, err := json.MarshalIndent(r, "", " ")
			if err != nil {
				log.Fatal(err)
			}

			err = ioutil.WriteFile(*results, file, 0644)
			if err != nil {
				log.Fatal(err)
			}
		}

	} else {
		_, err := buildpackage.Extract(*source, *destination, *id, *version)
		if err != nil {
			log.Fatal(err)
		}
	}

}
