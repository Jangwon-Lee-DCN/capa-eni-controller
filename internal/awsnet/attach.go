// Package awsnet connects reserved data interfaces without changing CAPA CRDs.
package awsnet

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

type api interface {
	DescribeNetworkInterfaces(context.Context, *ec2.DescribeNetworkInterfacesInput, ...func(*ec2.Options)) (*ec2.DescribeNetworkInterfacesOutput, error)
	DescribeInstances(context.Context, *ec2.DescribeInstancesInput, ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error)
	AttachNetworkInterface(context.Context, *ec2.AttachNetworkInterfaceInput, ...func(*ec2.Options)) (*ec2.AttachNetworkInterfaceOutput, error)
	ModifyNetworkInterfaceAttribute(context.Context, *ec2.ModifyNetworkInterfaceAttributeInput, ...func(*ec2.Options)) (*ec2.ModifyNetworkInterfaceAttributeOutput, error)
}

// Attacher uses the standard AWS credential chain and the pool's explicit Region.
type Attacher struct {
	Factory func(context.Context, string) (api, error)
}

func (a *Attacher) client(ctx context.Context, region string) (api, error) {
	if a.Factory != nil {
		return a.Factory(ctx, region)
	}
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return nil, err
	}
	return ec2.NewFromConfig(cfg), nil
}

// Ensure is idempotent and never detaches or takes over an occupied interface/slot.
func (a *Attacher) Ensure(ctx context.Context, region, vpc, id, primary, instanceID string, index int) (bool, error) {
	c, err := a.client(ctx, region)
	if err != nil {
		return false, err
	}
	result, err := c.DescribeInstances(ctx, &ec2.DescribeInstancesInput{InstanceIds: []string{instanceID}})
	if err != nil {
		return false, err
	}
	if len(result.Reservations) != 1 || len(result.Reservations[0].Instances) != 1 {
		return false, fmt.Errorf("expected one instance")
	}
	instance := result.Reservations[0].Instances[0]
	if aws.ToString(instance.VpcId) != vpc {
		return false, fmt.Errorf("instance VPC does not match pool")
	}
	if instance.State == nil || instance.State.Name != types.InstanceStateNameRunning {
		return false, nil
	}
	if ready, err := slotsReady(instance, id, index); err != nil || !ready {
		return false, err
	}
	interfaces, err := c.DescribeNetworkInterfaces(ctx, &ec2.DescribeNetworkInterfacesInput{NetworkInterfaceIds: []string{id}})
	if err != nil {
		return false, err
	}
	if len(interfaces.NetworkInterfaces) != 1 {
		return false, fmt.Errorf("expected one network interface")
	}
	n := interfaces.NetworkInterfaces[0]
	if aws.ToString(n.VpcId) != vpc || aws.ToString(n.PrivateIpAddress) != primary || instance.Placement == nil || aws.ToString(n.AvailabilityZone) != aws.ToString(instance.Placement.AvailabilityZone) {
		return false, fmt.Errorf("ENI VPC, primary IP or AZ does not match reserved plan")
	}
	attachment := n.Attachment
	if attachment == nil {
		if index == 0 {
			return false, fmt.Errorf("CAPA primary ENI is not attached")
		}
		attached, err := c.AttachNetworkInterface(ctx, &ec2.AttachNetworkInterfaceInput{NetworkInterfaceId: aws.String(id), InstanceId: aws.String(instanceID), DeviceIndex: aws.Int32(int32(index))})
		if err != nil {
			return false, err
		}
		_, err = c.ModifyNetworkInterfaceAttribute(ctx, &ec2.ModifyNetworkInterfaceAttributeInput{NetworkInterfaceId: aws.String(id), Attachment: &types.NetworkInterfaceAttachmentChanges{AttachmentId: attached.AttachmentId, DeleteOnTermination: aws.Bool(false)}})
		return false, err
	}
	if aws.ToString(attachment.InstanceId) != instanceID || aws.ToInt32(attachment.DeviceIndex) != int32(index) {
		return false, fmt.Errorf("reserved ENI is attached elsewhere")
	}
	if attachment.Status != types.AttachmentStatusAttached {
		return false, nil
	}
	if aws.ToBool(attachment.DeleteOnTermination) {
		_, err = c.ModifyNetworkInterfaceAttribute(ctx, &ec2.ModifyNetworkInterfaceAttributeInput{NetworkInterfaceId: aws.String(id), Attachment: &types.NetworkInterfaceAttachmentChanges{AttachmentId: attachment.AttachmentId, DeleteOnTermination: aws.Bool(false)}})
		if err != nil {
			return false, err
		}
	}
	return true, nil
}

func slotsReady(instance types.Instance, id string, index int) (bool, error) {
	for previous := range index {
		found := false
		for _, n := range instance.NetworkInterfaces {
			if n.Attachment != nil && aws.ToInt32(n.Attachment.DeviceIndex) == int32(previous) && n.Attachment.Status == types.AttachmentStatusAttached {
				found = true
			}
		}
		if !found {
			return false, nil
		}
	}
	for _, n := range instance.NetworkInterfaces {
		if n.Attachment != nil && aws.ToInt32(n.Attachment.DeviceIndex) == int32(index) && aws.ToString(n.NetworkInterfaceId) != id {
			return false, fmt.Errorf("device index %d is occupied", index)
		}
	}
	return true, nil
}
