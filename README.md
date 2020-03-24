# Package Extractor

A simple tool to "extract" buildpackages out of an existing builder.

 
## Install
```bash
go get github.com/matthewmcnew/package-extractor
```
  
## Usage

## Extracting a single buildpack 
```bash
package-extractor -from=cloudfoundry/cnb:bionic -to=gcr.io/some-where-I-can-write-to -id=org.cloudfoundry.nodejs -version=v2.0.0
```

* `-from` Builder to extract from
* `-to` Location to write buildpackage to (Must have write access)
* `-id` id of buildpack for buildpackage
* `-version` version of buildpack for buildpackage

## Extracting all buildpacks from a builder

```bash
package-extractor -from=cloudfoundry/cnb:bionic -to=gcr.io/some-registry-to-write-to --builder-all=true --results <OUTPUT/METADATA_PATH>
```

* `-from` Builder to extract from
* `-to` Location to write buildpackage to (Must have write access)
* `builder-all=true` enables reading all top level buildpacks from a builder
* `-results` output path to write metadata about extracted buildpacks 
