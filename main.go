package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"sort"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/pivotal/kpack/pkg/registry/imagehelpers"
	"github.com/pkg/errors"
)

const (
	layersMetadata       = "io.buildpacks.buildpack.layers"
	buildPackageMetadata = "io.buildpacks.buildpackage.metadata"
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
		r, err := extractAll(*source, *destination)
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
		_, err := extract(*source, *destination, *id, *version)
		if err != nil {
			log.Fatal(err)
		}
	}

}

type BuildPackage struct {
	Image       string  `json:"image"`
	Description string  `json:"description"`
	Id          string  `json:"id"`
	Version     string  `json:"version"`
	Digest      string  `json:"digest"`
	Stacks      []Stack `json:"stacks"`
	Tag         string  `json:"tag"`
}

type Results struct {
	BuildPackages []BuildPackage `json:"buildpackages"`
	Order         Order          `json:"order"`
	Source        string         `json:"source"`
}

func (r *Results) appendBuildPackage(i BuildPackage) {
	for _, b := range r.BuildPackages {
		if b.Image == i.Image {
			return
		}
	}
	r.BuildPackages = append(r.BuildPackages, i)
}

func extractAll(from, to string) (Results, error) {
	results := Results{
		Order:  Order{},
		Source: from,
	}

	reference, err := name.ParseReference(from)
	if err != nil {
		return results, err
	}

	image, err := remote.Image(reference, remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		return results, err
	}

	err = imagehelpers.GetLabel(image, "io.buildpacks.buildpack.order", &results.Order)
	if err != nil {
		return results, err
	}

	for _, g := range results.Order {
		for _, b := range g.Group {
			toRef, err := name.ParseReference(to)
			if err != nil {
				return results, err
			}

			dest := fmt.Sprintf("%s/%s/%s",
				toRef.Context().RegistryStr(),
				toRef.Context().RepositoryStr(),
				strings.ReplaceAll(b.Id, ".", ""))

			i, err := extract(from, dest, b.Id, "")
			if err != nil {
				return results, err
			}

			results.appendBuildPackage(i)
		}
	}
	return results, nil
}

func extract(from, to, id, version string) (BuildPackage, error) {
	reference, err := name.ParseReference(from)
	if err != nil {
		return BuildPackage{}, err
	}

	image, err := remote.Image(reference, remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		return BuildPackage{}, err
	}

	metadata := BuildpackLayerMetadata{}
	err = imagehelpers.GetLabel(image, layersMetadata, &metadata)
	if err != nil {
		return BuildPackage{}, err
	}

	buildpackageMetadata, version, err := metadata.metadataFor(BuildpackLayerMetadata{}, id, version)
	if err != nil {
		return BuildPackage{}, err
	}

	buildpackage, err := random.Image(0, 0)
	if err != nil {
		return BuildPackage{}, err
	}

	for _, info := range determinsticSort(buildpackageMetadata) {
		hash, err := v1.NewHash(info.LayerDiffID)
		if err != nil {
			return BuildPackage{}, err
		}

		layer, err := image.LayerByDiffID(hash)
		if err != nil {
			return BuildPackage{}, err
		}

		buildpackage, err = mutate.AppendLayers(buildpackage, layer)
		if err != nil {
			return BuildPackage{}, err
		}
	}

	buildpackage, err = imagehelpers.SetLabels(buildpackage, map[string]interface{}{
		layersMetadata: buildpackageMetadata,
		buildPackageMetadata: Metadata{
			BuildpackInfo: BuildpackInfo{
				Id:      id,
				Version: version,
			},
			Stacks: buildpackageMetadata[id][version].Stacks,
		},
	})
	if err != nil {
		return BuildPackage{}, err
	}

	reference, err = name.ParseReference(to)
	if err != nil {
		return BuildPackage{}, err
	}

	err = remote.Write(reference, buildpackage, remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		return BuildPackage{}, err
	}

	err = remote.Tag(reference.Context().Tag(version), buildpackage, remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		return BuildPackage{}, err
	}

	digest, err := buildpackage.Digest()
	if err != nil {
		return BuildPackage{}, err
	}

	description := fmt.Sprintf("%s@%s", id, version)
	result := fmt.Sprintf("%s@%s", to, digest.String())
	log.Printf("successfully wrote %s to %s", description, result)

	return BuildPackage{
		Image:       result,
		Description: description,
		Id:          id,
		Version:     version,
		Digest:      digest.String(),
		Stacks:      buildpackageMetadata[id][version].Stacks,
		Tag:         version,
	}, nil
}

type BuildpackLayerMetadata map[string]map[string]BuildpackLayerInfo

func (m BuildpackLayerMetadata) metadataFor(initalMetadata BuildpackLayerMetadata, id string, version string) (BuildpackLayerMetadata, string, error) {
	bps, ok := m[id]
	if !ok {
		var available []string

		for bp := range m {
			available = append(available, bp)
		}

		return nil, "", errors.Errorf("could not find %s, options: %s", id, available)
	}

	version, err := pickVersion(version, bps)
	if err != nil {
		return nil, "", errors.Wrapf(err, "picking version for buildpack %s", id)
	}

	info, ok := bps[version]
	if !ok {
		var available []string

		for v := range bps {
			available = append(available, fmt.Sprintf("%s@%s", id, v))
		}

		return nil, "", errors.Errorf("could not find %s@%s, options: %s", id, version, available)
	}

	_, ok = initalMetadata[id]
	if !ok {
		initalMetadata[id] = map[string]BuildpackLayerInfo{}
	}
	initalMetadata[id][version] = info

	for _, oe := range info.Order {
		for _, g := range oe.Group {

			var err error
			initalMetadata, _, err = m.metadataFor(initalMetadata, g.Id, g.Version)
			if err != nil {
				return nil, "", err
			}
		}
	}

	return initalMetadata, version, nil
}

func pickVersion(version string, bps map[string]BuildpackLayerInfo) (string, error) {
	if version != "" {
		return version, nil
	}

	if len(bps) != 1 {
		return "", errors.Errorf("more than one version available")
	}

	for v := range bps {
		return v, nil
	}

	return "", errors.Errorf("error picking version")
}

func determinsticSort(metadata BuildpackLayerMetadata) []BuildpackLayerInfo {
	layers := []BuildpackLayerInfo{}
	for _, bps := range metadata {
		for _, info := range bps {
			layers = append(layers, info)
		}
	}

	sort.Slice(layers, func(i, j int) bool {
		return layers[i].LayerDiffID > layers[j].LayerDiffID
	})

	return layers
}

type BuildpackLayerInfo struct {
	API         string  `json:"api"`
	Stacks      []Stack `json:"stacks,omitempty"`
	Order       Order   `json:"order,omitempty"`
	LayerDiffID string  `json:"layerDiffID"`
}

type Order []OrderEntry

type OrderEntry struct {
	Group []BuildpackRef `json:"group,omitempty"`
}

type BuildpackRef struct {
	BuildpackInfo `json:",inline"`
	Optional      bool `json:"optional,omitempty"`
}

type BuildpackInfo struct {
	Id      string `json:"id"`
	Version string `json:"version,omitempty"`
}

type Stack struct {
	ID     string   `json:"id"`
	Mixins []string `json:"mixins,omitempty"`
}

type Metadata struct {
	BuildpackInfo
	Stacks []Stack `toml:"stacks" json:"stacks,omitempty"`
}
