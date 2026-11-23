// Package compat adapts only the CAPI/CAPA fields used by ENI allocation.
package compat

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Client selects a served API per kind instead of requiring a provider upgrade.
// Reads project the shared allocator contract; patches retain the complete wire object.
type Client struct {
	client.Client
	Versions map[string]schema.GroupVersionKind
}

// New discovers each kind independently, supporting mixed CAPI/CAPA generations.
func New(base client.Client, mapper meta.RESTMapper) (*Client, error) {
	c := &Client{Client: base, Versions: map[string]schema.GroupVersionKind{}}
	for kind, group := range map[string]string{awsMachineKind: infraGroup, awsClusterKind: infraGroup, machineKind: capiGroup, clusterKind: capiGroup} {
		mapping, err := mapper.RESTMapping(schema.GroupKind{Group: group, Kind: kind}, currentVersion, legacyVersion)
		if err != nil {
			return nil, fmt.Errorf("discover %s served versions: %w", kind, err)
		}
		c.Versions[kind] = mapping.GroupVersionKind
	}
	return c, nil
}

// Object returns the actual served GVK for cache watches and API requests.
func (c *Client) Object(kind string) *unstructured.Unstructured {
	o := &unstructured.Unstructured{}
	o.SetGroupVersionKind(c.Versions[kind])
	return o
}

func (c *Client) target(obj client.Object) (string, bool) {
	gvk, _, err := c.Scheme().ObjectKinds(obj)
	if err != nil {
		return "", false
	}
	for _, g := range gvk {
		if v, ok := c.Versions[g.Kind]; ok && v.Group == g.Group {
			return g.Kind, true
		}
	}
	return "", false
}

// Get reads the served version without depending on its Go API package.
func (c *Client) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	kind, ok := c.target(obj)
	if !ok {
		return c.Client.Get(ctx, key, obj, opts...)
	}
	wire := c.Object(kind)
	if err := c.Client.Get(ctx, key, wire, opts...); err != nil {
		return err
	}
	projected := map[string]any{metadataField: wire.Object[metadataField]}
	spec := map[string]any{}
	projected[specField] = spec
	paths := map[string][][]string{
		awsMachineKind: {{networkInterfacesField}, {"instanceID"}, {"providerID"}}, awsClusterKind: {{"region"}, {"network", "vpc", "id"}},
		machineKind: {{"clusterName"}}, clusterKind: {{"infrastructureRef"}},
	}
	for _, path := range paths[kind] {
		value, found, err := unstructured.NestedFieldCopy(wire.Object, append([]string{specField}, path...)...)
		if err != nil {
			return err
		}
		if found {
			if err = unstructured.SetNestedField(projected, value, append([]string{specField}, path...)...); err != nil {
				return err
			}
		}
	}
	if kind == clusterKind {
		ref, found, err := unstructured.NestedMap(projected, specField, "infrastructureRef")
		if err != nil {
			return err
		}
		if found {
			if _, ok := ref["apiGroup"]; !ok {
				if v, ok := ref["apiVersion"].(string); ok {
					ref["apiGroup"] = strings.SplitN(v, "/", 2)[0]
				}
			}
			if err = unstructured.SetNestedMap(projected, ref, specField, "infrastructureRef"); err != nil {
				return err
			}
		}
	}
	return runtime.DefaultUnstructuredConverter.FromUnstructured(projected, obj)
}

// Patch sends only the caller's merge patch to the served resource. Old fields survive.
func (c *Client) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	kind, ok := c.target(obj)
	if !ok {
		return c.Client.Patch(ctx, obj, patch, opts...)
	}
	data, err := patch.Data(obj)
	if err != nil {
		return err
	}
	wire := c.Object(kind)
	wire.SetName(obj.GetName())
	wire.SetNamespace(obj.GetNamespace())
	return c.Client.Patch(ctx, wire, client.RawPatch(patch.Type(), data), opts...)
}
