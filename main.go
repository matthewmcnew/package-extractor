package main

import (
	"flag"
	"fmt"
	"log"
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

var (
	source         = flag.String("from", "", "location to extract from")
	destination    = flag.String("to", "", "builpackage location to write to")
	tarDestination = flag.String("to-file", "", "file location to write to")
	id             = flag.String("id", "", "id to extract")
	version        = flag.String("version", "", "version to extract")
	builderAll     = flag.String("builder-all", "", "extract all buildpacks from builder")
)

func main() {
	flag.Parse()

	if *builderAll != "" {
		err := extractAll(*source, *destination)
		if err != nil {
			log.Fatal(err)
		}
	} else {
		err := extract(*source, *destination, *id, *version)
		if err != nil {
			log.Fatal(err)
		}
	}

}

func extractAll(from, to string) error {
	reference, err := name.ParseReference(from)
	if err != nil {
		return err
	}

	image, err := remote.Image(reference, remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		return err
	}

	order := Order{}
	err = imagehelpers.GetLabel(image, "io.buildpacks.buildpack.order", &order)
	if err != nil {
		return err
	}

	for _, g := range order {
		for _, b := range g.Group {
			toRef, err := name.ParseReference(to)
			if err != nil {
				return err
			}

			dest := fmt.Sprintf("%s/%s/%s",
				toRef.Context().RegistryStr(),
				toRef.Context().RepositoryStr(),
				strings.ReplaceAll(b.Id, ".", ""))

			err = extract(from, dest, b.Id, "")
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func extract(from, to, id, version string) error {

	reference, err := name.ParseReference(from)
	if err != nil {
		return err
	}

	image, err := remote.Image(reference, remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		return err
	}

	metadata := BuildpackLayerMetadata{}
	err = imagehelpers.GetLabel(image, "io.buildpacks.buildpack.layers", &metadata)
	if err != nil {
		return err
	}

	buildpackageMetadata, version, err := metadata.metadataFor(BuildpackLayerMetadata{}, id, version)
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
