package buildpackage

import (
	"fmt"
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

	"github.com/matthewmcnew/package-extractor/stack"
)

const (
	LayersMetadata       = "io.buildpacks.buildpack.layers"
	BuildPackageMetadata = "io.buildpacks.buildpackage.metadata"
	OrderLabel           = "io.buildpacks.buildpack.order"
)

type BuildPackage struct {
	Image       string        `json:"image"`
	Description string        `json:"description"`
	Id          string        `json:"id"`
	Version     string        `json:"version"`
	Digest      string        `json:"digest"`
	Stacks      []stack.Stack `json:"stacks"`
	Tag         string        `json:"tag"`
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

func ExtractAll(from, to string) (Results, error) {
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

	err = imagehelpers.GetLabel(image, OrderLabel, &results.Order)
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

			i, err := Extract(from, dest, b.Id, "")
			if err != nil {
				return results, err
			}

			results.appendBuildPackage(i)
		}
	}
	return results, nil
}

func Extract(from, to, id, version string) (BuildPackage, error) {
	reference, err := name.ParseReference(from)
	if err != nil {
		return BuildPackage{}, err
	}

	image, err := remote.Image(reference, remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		return BuildPackage{}, err
	}

	metadata := BuildpackLayerMetadata{}
	err = imagehelpers.GetLabel(image, LayersMetadata, &metadata)
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
		LayersMetadata: buildpackageMetadata,
		BuildPackageMetadata: Metadata{
			BuildpackInfo: BuildpackInfo{
				Id:       id,
				Version:  version,
				Homepage: buildpackageMetadata[id][version].Homepage,
			},
			Stacks: calculateStack(buildpackageMetadata),
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
		Stacks:      calculateStack(buildpackageMetadata),
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

func calculateStack(metadata BuildpackLayerMetadata) []stack.Stack {
	var stacks []stack.Stack
	for _, bpi := range metadata {
		for _, bp := range bpi {
			if len(stacks) == 0 {
				stacks = bp.Stacks
			} else if len(bp.Stacks) > 0 { // skip over "meta-buildpacks"
				stacks = stack.MergeCompatible(stacks, bp.Stacks)
			}
		}
	}

	sort.Slice(stacks, func(i, j int) bool {
		return stacks[i].ID > stacks[j].ID
	})

	return stacks
}

func determinsticSort(metadata BuildpackLayerMetadata) []BuildpackLayerInfo {
	var layers []BuildpackLayerInfo
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
	API         string        `json:"api"`
	Stacks      []stack.Stack `json:"stacks,omitempty"`
	Order       Order         `json:"order,omitempty"`
	LayerDiffID string        `json:"layerDiffID"`
	Homepage    string        `json:"homepage,omitempty"`
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
	Id       string `json:"id"`
	Version  string `json:"version,omitempty"`
	Homepage string `json:"homepage,omitempty"`
}

type Metadata struct {
	BuildpackInfo
	Stacks []stack.Stack `toml:"stacks" json:"stacks,omitempty"`
}
