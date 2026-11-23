package v1beta2

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	networkingv1alpha1 "github.com/ssu-dcn/capa-eni-controller/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	infrastructurev1beta2 "sigs.k8s.io/cluster-api-provider-aws/v2/api/v1beta2"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// additionalInterfaceKeys preserves the declared device order after the primary ENI.
func (d *AWSMachineCustomDefaulter) additionalInterfaceKeys(ctx context.Context, obj *infrastructurev1beta2.AWSMachine) ([]string, error) {
	value, explicit := obj.Annotations[networkingv1alpha1.AdditionalInterfaceKeysAnnotation]
	if !explicit {
		for _, owner := range obj.OwnerReferences {
			gv, err := schema.ParseGroupVersion(owner.APIVersion)
			if err != nil || owner.Kind != capiMachineKind || gv.Group != clusterv1.GroupVersion.Group {
				continue
			}
			machine := &clusterv1.Machine{}
			if err = d.Client.Get(ctx, client.ObjectKey{Namespace: obj.Namespace, Name: owner.Name}, machine); err != nil {
				return nil, err
			}
			value = machine.Annotations[networkingv1alpha1.AdditionalInterfaceKeysAnnotation]
			break
		}
	}
	if value == "" {
		return nil, nil
	}
	keys := strings.Split(value, ",")
	for i := range keys {
		keys[i] = strings.TrimSpace(keys[i])
		if keys[i] == "" {
			return nil, fmt.Errorf("additional-interface-keys contains an empty key")
		}
	}
	return keys, nil
}

// claimInterfaceSet reserves all ENIs, or releases only this admission attempt's new claims.
// Claims are reused only within the same admission request, never across concurrent creates.
// A fixed bundle never falls back to dynamic networking, irrespective of pool policy.
func (d *AWSMachineCustomDefaulter) claimInterfaceSet(ctx context.Context, obj *infrastructurev1beta2.AWSMachine, pool *networkingv1alpha1.ENIPool, keys []string) (ids []string, err error) {
	if len(keys) < 2 || keys[0] == "" {
		return nil, fmt.Errorf("an ENI bundle requires an explicit primary interface-key")
	}
	byKey := map[string]networkingv1alpha1.ENIReference{}
	for _, eni := range pool.Spec.Interfaces {
		if _, exists := byKey[eni.Key]; exists && eni.Key != "" {
			return nil, fmt.Errorf("duplicate ENIPool key %q", eni.Key)
		}
		byKey[eni.Key] = eni
	}
	seenKeys, seenIDs := map[string]bool{}, map[string]bool{}
	for _, key := range keys {
		eni, ok := byKey[key]
		if !ok || key == "" || seenKeys[key] || seenIDs[eni.ID] {
			return nil, fmt.Errorf("unknown or duplicate ENI bundle key %q", key)
		}
		seenKeys[key] = true
		seenIDs[eni.ID] = true
		ids = append(ids, eni.ID)
	}
	requestID := obj.Namespace + "/" + obj.Name
	if request, e := admission.RequestFromContext(ctx); e == nil && request.UID != "" {
		requestID = string(request.UID)
	}
	created := []*networkingv1alpha1.ENIClaim{}
	defer func() {
		if err != nil {
			err = errors.Join(err, d.releaseNewClaims(ctx, obj, created, requestID))
		}
	}()
	for _, id := range ids {
		claim := &networkingv1alpha1.ENIClaim{ObjectMeta: metav1.ObjectMeta{Name: id, Finalizers: []string{networkingv1alpha1.ClaimFinalizer}, Annotations: map[string]string{requestAnnotation: requestID}}, Spec: networkingv1alpha1.ENIClaimSpec{MachineRef: networkingv1alpha1.NamespacedObjectReference{Namespace: obj.Namespace, Name: obj.Name}, PoolRef: networkingv1alpha1.ClusterObjectReference{Name: pool.Name}, ENIID: id}}
		if e := d.Client.Create(ctx, claim); e != nil {
			if !apierrors.IsAlreadyExists(e) {
				return nil, e
			}
			existing := &networkingv1alpha1.ENIClaim{}
			if e = d.Client.Get(ctx, client.ObjectKey{Name: id}, existing); e != nil {
				return nil, e
			}
			if existing.Spec.MachineRef != claim.Spec.MachineRef || existing.Spec.PoolRef != claim.Spec.PoolRef || existing.DeletionTimestamp != nil || existing.Annotations[requestAnnotation] != requestID {
				return nil, fmt.Errorf("ENI %s is already reserved", id)
			}
		} else {
			created = append(created, claim)
		}
	}
	return ids, nil
}

const requestAnnotation = "eni.dcn.ssu.ac.kr/admission-request-id"

func (d *AWSMachineCustomDefaulter) releaseNewClaims(ctx context.Context, obj *infrastructurev1beta2.AWSMachine, created []*networkingv1alpha1.ENIClaim, requestID string) (err error) {
	for _, v := range slices.Backward(created) {
		c := v
		// Remove the allocation finalizer before deleting a reservation that never bound to an AWSMachine.
		if e := d.Client.Get(ctx, client.ObjectKeyFromObject(c), c); apierrors.IsNotFound(e) {
			continue
		} else if e != nil {
			err = errors.Join(err, e)
			continue
		}
		if c.Spec.MachineRef.Name != obj.Name || c.Spec.MachineRef.Namespace != obj.Namespace || c.Annotations[requestAnnotation] != requestID {
			err = errors.Join(err, fmt.Errorf("claim owner changed during rollback"))
			continue
		}
		before := c.DeepCopy()
		finalizers := c.Finalizers[:0]
		for _, f := range c.Finalizers {
			if f != networkingv1alpha1.ClaimFinalizer {
				finalizers = append(finalizers, f)
			}
		}
		c.Finalizers = finalizers
		if e := d.Client.Patch(ctx, c, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{})); e != nil {
			err = errors.Join(err, e)
			continue
		}
		if e := d.Client.Delete(ctx, c, client.Preconditions{UID: &c.UID}); e != nil && !apierrors.IsNotFound(e) {
			err = errors.Join(err, e)
		}
	}
	return err
}
