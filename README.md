# Package Extractor

A simple tool to "extract" buildpackages out of an existing builder.

 
### Install
```bash
go get github.com/matthewmcnew/package-extractor
```
  
### Usage

```bash
package-extractor -from=cloudfoundry/cnb:bionic -to=gcr.io/some-where-I-can-write-to -id=org.cloudfoundry.nodejs -version=v2.0.0
```

All flags are required

* `-from` Builder to extract from
* `-to` Location to write buildpackage to (Must have write access)
* `-id` id of buildpack for buildpackage
* `-version` version of buildpack for buildpackage


