package compat

const (
	awsMachineKind = "AWSMachine"
	awsClusterKind = "AWSCluster"
	machineKind    = "Machine"
	clusterKind    = "Cluster"
	infraGroup     = "infrastructure.cluster.x-k8s.io"
	capiGroup      = "cluster.x-k8s.io"
	metadataField  = "metadata"
	specField      = "spec"
	kindField      = "kind"
	legacyVersion  = "v1beta1"
	currentVersion = "v1beta2"
)

const networkInterfacesField = "networkInterfaces"
