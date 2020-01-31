package main

import (
	"flag"
	"fmt"
	"log"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/pivotal/kpack/pkg/registry/imagehelpers"
	"github.com/pkg/errors"
)

var (
	source      = flag.String("from", "", "location to extract from")
	destination = flag.String("to", "", "builpackage location to write to")
	id          = flag.String("id", "", "id to extract")
	version     = flag.String("version", "", "version to extract")
)

func main() {
	flag.Parse()

	err := extract(*source, *destination, *id, *version)
	if err != nil {

		log.Fatal(err)
	}
}

func extract(from, to, id, version string) error {
	reference, err := name.ParseReference(from)
	if err != nil {
		return err
	}

	image, err := remote.Image(reference)
	if err != nil {
		return err
	}

	metadata := BuildpackLayerMetadata{}
	err = imagehelpers.GetLabel(image, "io.buildpacks.buildpack.layers", &metadata)
	if err != nil {
		return err
	}

	buildpackageMetadata, err := metadata.metadataFor(BuildpackLayerMetadata{}, id, version)
	if err != nil {
		return err
	}

	buildpackage, err := random.Image(0, 0)
	if err != nil {
		return err
	}

	for _, bps := range buildpackageMetadata {
		for _, info := range bps {
			hash, err := v1.NewHash(info.LayerDiffID)
			if err != nil {
				return err
			}

			layer, err := image.LayerByDiffID(hash)
			if err != nil {
				return err
			}

			buildpackage, err = mutate.AppendLayers(buildpackage, layer)
			if err != nil {
				return err
			}
		}
	}

	buildpackage, err = imagehelpers.SetLabels(buildpackage, map[string]interface{}{
		"io.buildpacks.buildpack.layers": buildpackageMetadata,
		"io.buildpacks.buildpackage.metadata": Metadata{
			BuildpackInfo: BuildpackInfo{
				Id:      id,
				Version: version,
			},
			Stacks: buildpackageMetadata[id][version].Stacks,
		},
	})
	if err != nil {
		return err
	}

	reference, err = name.ParseReference(to)
	if err != nil {
		return err
	}

	err = remote.Write(reference, buildpackage, remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		return err
	}

	digest, err := buildpackage.Digest()
	if err != nil {
		return err
	}

	log.Print(fmt.Sprintf("successfully wrote %s@%s to %s@%s", id, version, to, digest.String()))
	return nil
}

type BuildpackLayerMetadata map[string]map[string]BuildpackLayerInfo

func (m BuildpackLayerMetadata) metadataFor(initalMetadata BuildpackLayerMetadata, id string, version string) (BuildpackLayerMetadata, error) {
	bps, ok := m[id]
	if !ok {
		return nil, errors.Errorf("could not find %s", id)
	}

	info, ok := bps[version]
	if !ok {
		return nil, errors.Errorf("could not find %s@%s", id, version)
	}

	_, ok = initalMetadata[id]
	if !ok {
		initalMetadata[id] = map[string]BuildpackLayerInfo{}
	}
	initalMetadata[id][version] = info

	for _, oe := range info.Order {
		for _, g := range oe.Group {

			var err error
			initalMetadata, err = m.metadataFor(initalMetadata, g.Id, g.Version)
			if err != nil {
				return nil, err
			}
		}
	}

	return initalMetadata, nil
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
