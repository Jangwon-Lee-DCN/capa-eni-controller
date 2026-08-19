package v1alpha1

const (
	AllocateFromPoolAnnotation = "eni.dcn.ssu.ac.kr/allocate-from-pool"
	AllocatorPausedAnnotation  = "eni.dcn.ssu.ac.kr/allocator-paused"
	AllocationResultAnnotation = "eni.dcn.ssu.ac.kr/allocation-result"
	AllocationReasonAnnotation = "eni.dcn.ssu.ac.kr/allocation-reason"
	CAPPausedAnnotation        = "cluster.x-k8s.io/paused"
	ClaimFinalizer             = "eniclaim.networking.dcn.ssu.ac.kr"
)

const (
	AllocationResultAllocated       = "Allocated"
	AllocationResultDynamicFallback = "DynamicFallback"
	AllocationResultFailed          = "Failed"
)
