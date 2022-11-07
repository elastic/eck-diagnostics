package filters

import (
	"fmt"
	"strings"

	"k8s.io/utils/strings/slices"
)

var (
	// ValidTypes are the valid types of Elastic resources that are supported by the filtering system.
	ValidTypes              = []string{"agent", "apm", "beat", "elasticsearch", "enterprisesearch", "kibana", "maps"}
	elasticTypeKey          = "common.k8s.elastic.co/type"
	elasticsearchNameFormat = "%s.k8s.elastic.co/cluster-name"
	elasticNameFormat       = "%s.k8s.elastic.co/name"
)

// Filter is a type + name filter that translates into a labelSelector that is applied when querying for Kubernetes resources.
// Both type and name are required at this time.
//
// examples supported:
// name=mycluster, type=elasticsearch
// name=mykb, type=kibana
type Filter struct {
	source        []string
	typ           string
	name          string
	labelSelector string
}

// LabelSelector returns the formatted labelSelector for the filter.
func (f Filter) LabelSelector() string {
	return f.labelSelector
}

// Type returns the type of the Elastic resources to filter.
func (f Filter) Type() string {
	return f.typ
}

// Name returns the name of the Elastic resource to filter.
func (f Filter) Name() string {
	return f.name
}

// New returns a new filter, given a slice of key=value pairs,
// runs validation on the given slice, and returns an error
// if the given key=value pairs are invalid.
//
// source example:
// []string{"name=mycluster", "type=elasticsearch"}
func New(source []string) (Filter, error) {
	filter := Filter{
		source: source,
	}
	return filter.validate()
}

func (f Filter) validate() (Filter, error) {
	if len(f.source) == 0 {
		return f, nil
	}
	var typ, name string
	for _, filter := range f.source {
		filterSlice := strings.Split(filter, "=")
		if len(filterSlice) != 2 {
			return f, fmt.Errorf("Invalid filter: %s", filter)
		}
		k, v := filterSlice[0], filterSlice[1]
		switch k {
		case "type":
			{
				if typ != "" {
					return f, fmt.Errorf("Only a single type filter is supported.")
				}
				typ = v
			}
		case "name":
			{
				if name != "" {
					return f, fmt.Errorf("Only a single name filter is supported.")
				}
				name = v
			}
		default:
			return f, fmt.Errorf("Invalid filter key: %s. Only 'type', and 'name' are supported.", k)
		}
	}
	if typ == "" {
		return f, fmt.Errorf("Invalid Filter: missing 'type'")
	}
	if err := validateType(typ); err != nil {
		return f, err
	}
	if name == "" {
		return f, fmt.Errorf("Invalid Filter: missing 'name'")
	}
	f.typ, f.name = typ, name
	f.labelSelector = convertFilterToLabelSelector(f.typ, f.name)
	return f, nil
}

func validateType(typ string) error {
	if !slices.Contains(ValidTypes, typ) {
		return fmt.Errorf("invalid type: %s, supported types: %v", typ, ValidTypes)
	}
	return nil
}

// convertFillterToLabelSelector will convert a given Elastic custom resource type,
// and name into a valid Kubernetes labelSelector.  The internal switch logic
// is required as Elasticsearch has a slighly different 'name' label format than other
// Elastic custom resource types ('cluster-name' vs 'name'):
//
// example
// "common.k8s.elastic.co/type=elasticsearch,elasticsearch.k8s.elastic.co/cluster-name=mycluster"
// vs
// "common.k8s.elastic.co/type=kibana,kibana.k8s.elastic.co/name=mykb"
func convertFilterToLabelSelector(typ, name string) string {
	var elasticfilter string
	elasticfilter += elasticTypeKey + "=" + strings.ToLower(typ) + ","
	switch typ {
	case "elasticsearch":
		elasticfilter += fmt.Sprintf(elasticsearchNameFormat, strings.ToLower(typ)) + "=" + name
	default:
		elasticfilter += fmt.Sprintf(elasticNameFormat, strings.ToLower(typ)) + "=" + name
	}

	return elasticfilter
}
