package stack

import "sort"

// https://github.com/buildpacks/pack/blob/abf3235e2bedab0928060bb9805151fe81e3b590/internal/stack/merge.go#L37
func MergeCompatible(stacksA []Stack, stacksB []Stack) []Stack {
	set := map[string][]string{}

	for _, s := range stacksA {
		set[s.ID] = s.Mixins
	}

	var results []Stack
	for _, s := range stacksB {
		if stackMixins, ok := set[s.ID]; ok {
			mixin := uniqueStrings(append(stackMixins, s.Mixins...))

			results = append(results, Stack{
				ID:     s.ID,
				Mixins: mixin,
			})
		}
	}

	return results
}

func uniqueStrings(values []string) []string {
	set := map[string]interface{}{}
	for _, s := range values {
		set[s] = nil
	}
	var uniqueStrings []string
	for s := range set {
		uniqueStrings = append(uniqueStrings, s)
	}

	sort.Strings(uniqueStrings)

	return uniqueStrings
}
