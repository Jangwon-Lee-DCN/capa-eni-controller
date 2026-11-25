package awsnet

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

type fakeEC2 struct {
	instance  types.Instance
	eni       types.NetworkInterface
	attached  int
	preserved int
}

func (f *fakeEC2) DescribeInstances(context.Context, *ec2.DescribeInstancesInput, ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
	return &ec2.DescribeInstancesOutput{Reservations: []types.Reservation{{Instances: []types.Instance{f.instance}}}}, nil
}
func (f *fakeEC2) DescribeNetworkInterfaces(context.Context, *ec2.DescribeNetworkInterfacesInput, ...func(*ec2.Options)) (*ec2.DescribeNetworkInterfacesOutput, error) {
	return &ec2.DescribeNetworkInterfacesOutput{NetworkInterfaces: []types.NetworkInterface{f.eni}}, nil
}
func (f *fakeEC2) AttachNetworkInterface(_ context.Context, in *ec2.AttachNetworkInterfaceInput, _ ...func(*ec2.Options)) (*ec2.AttachNetworkInterfaceOutput, error) {
	f.attached++
	f.eni.Attachment = &types.NetworkInterfaceAttachment{AttachmentId: aws.String("attach-1"), InstanceId: in.InstanceId, DeviceIndex: in.DeviceIndex, Status: types.AttachmentStatusAttached}
	return &ec2.AttachNetworkInterfaceOutput{AttachmentId: aws.String("attach-1")}, nil
}
func (f *fakeEC2) ModifyNetworkInterfaceAttribute(_ context.Context, in *ec2.ModifyNetworkInterfaceAttributeInput, _ ...func(*ec2.Options)) (*ec2.ModifyNetworkInterfaceAttributeOutput, error) {
	if !aws.ToBool(in.Attachment.DeleteOnTermination) {
		f.preserved++
	}
	return &ec2.ModifyNetworkInterfaceAttributeOutput{}, nil
}

func TestAttachmentSafetyAndRetry(t *testing.T) {
	fixture := func() *fakeEC2 {
		return &fakeEC2{instance: types.Instance{VpcId: aws.String("vpc-demo"), State: &types.InstanceState{Name: types.InstanceStateNameRunning}, Placement: &types.Placement{AvailabilityZone: aws.String("ap-northeast-2a")}, NetworkInterfaces: []types.InstanceNetworkInterface{{NetworkInterfaceId: aws.String("eni-primary"), Attachment: &types.InstanceNetworkInterfaceAttachment{DeviceIndex: aws.Int32(0), Status: types.AttachmentStatusAttached}}}}, eni: types.NetworkInterface{VpcId: aws.String("vpc-demo"), PrivateIpAddress: aws.String("172.31.90.10"), AvailabilityZone: aws.String("ap-northeast-2a")}}
	}
	t.Run("attach-and-repeat", func(t *testing.T) {
		f := fixture()
		a := &Attacher{Factory: func(context.Context, string) (api, error) { return f, nil }}
		ready, err := a.Ensure(context.Background(), "ap-northeast-2", "vpc-demo", "eni-demo", "172.31.90.10", "i-demo", 1)
		if err != nil || ready || f.attached != 1 || f.preserved != 1 {
			t.Fatalf("bad attachment: %v %v %#v", ready, err, f)
		}
		ready, err = a.Ensure(context.Background(), "ap-northeast-2", "vpc-demo", "eni-demo", "172.31.90.10", "i-demo", 1)
		if err != nil || !ready || f.attached != 1 {
			t.Fatalf("retry duplicated attachment: %v %v", ready, err)
		}
	})
	t.Run("wait-for-earlier-index", func(t *testing.T) {
		f := fixture()
		a := &Attacher{Factory: func(context.Context, string) (api, error) { return f, nil }}
		ready, err := a.Ensure(context.Background(), "ap-northeast-2", "vpc-demo", "eni-demo", "172.31.90.10", "i-demo", 2)
		if err != nil || ready || f.attached != 0 {
			t.Fatal("later interface attached before earlier index")
		}
	})
	for _, scenario := range []string{"wrong-vpc", "wrong-ip", "wrong-az", "other-instance", "occupied-index", "missing-primary"} {
		t.Run(scenario, func(t *testing.T) {
			f := fixture()
			index := 1
			switch scenario {
			case "wrong-vpc":
				f.instance.VpcId = aws.String("other")
			case "wrong-ip":
				f.eni.PrivateIpAddress = aws.String("172.31.90.99")
			case "wrong-az":
				f.eni.AvailabilityZone = aws.String("ap-northeast-2b")
			case "other-instance":
				f.eni.Attachment = &types.NetworkInterfaceAttachment{InstanceId: aws.String("i-other"), DeviceIndex: aws.Int32(1), Status: types.AttachmentStatusAttached}
			case "occupied-index":
				f.instance.NetworkInterfaces = append(f.instance.NetworkInterfaces, types.InstanceNetworkInterface{NetworkInterfaceId: aws.String("eni-other"), Attachment: &types.InstanceNetworkInterfaceAttachment{DeviceIndex: aws.Int32(1)}})
			case "missing-primary":
				index = 0
			}
			a := &Attacher{Factory: func(context.Context, string) (api, error) { return f, nil }}
			if _, err := a.Ensure(context.Background(), "ap-northeast-2", "vpc-demo", "eni-demo", "172.31.90.10", "i-demo", index); err == nil || f.attached != 0 {
				t.Fatal("unsafe attachment accepted")
			}
		})
	}
}
