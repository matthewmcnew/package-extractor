# Package Extractor

A simple tool to "extract" buildpackages out of an existing builder.

 
### Install
```bash
go get github.com/matthewmcnew/package-extractor
```
  
### Usage

```bash
package-extractor -from=cloudfoundry/cnb:bionic -to=gcr.io/some-where-I-can-write-to -id=org.cloudfoundry.nodejs -version=v1.0.0
```


