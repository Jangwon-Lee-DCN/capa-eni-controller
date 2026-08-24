# CAPA ENI Controller 설계

## 1. 배경

CAPA는 `AWSMachineTemplate`을 복제해 replica별 `AWSMachine`을 생성합니다.
템플릿의 `spec.networkInterfaces`에 ENI ID를 직접 넣으면 모든 replica가 같은
ENI를 사용하려 하므로 다중 replica 구성이 실패합니다.

이 프로젝트는 생성된 각 `AWSMachine`에 Pool의 서로 다른 ENI를 할당하되,
CAPA 리소스 생성 및 reconciliation 흐름을 유지합니다.

최종 런타임 topology와 sequence는 [`operation-flow.md`](operation-flow.md)를
참조하십시오.

## 2. 설계 원칙

1. 명시적으로 opt-in한 `AWSMachine`만 처리한다.
2. 사용자는 Region, VPC ID, ENI ID와 기대하는 primary private IPv4만 선언한다.
3. AZ와 subnet은 사용자 입력으로 받지 않는다.
4. Webhook과 Controller 모두 AWS API를 호출하지 않는다.
5. ENI 선택과 예약은 AWSMachine CREATE admission에서, 상태 갱신과 회수는
   Controller reconciliation에서 수행한다.
6. Pool이 고갈되면 기본 정책상 CAPA의 동적 IP 할당으로 진행한다.
7. 동시에 생성되는 Machine들이 같은 ENI를 할당받지 않도록 원자적 Claim을 쓴다.

## 3. API와 식별자

### 3.1 API group

```text
networking.dcn.ssu.ac.kr
```

초기 API version은 `v1alpha1`입니다.

### 3.2 Annotation

| Key | 대상 | 의미 |
| --- | --- | --- |
| `eni.dcn.ssu.ac.kr/allocate-from-pool` | `AWSMachine` | `true`이면 ENI 할당 대상 |
| `eni.dcn.ssu.ac.kr/interface-key` | `Machine` 또는 `AWSMachine` | 같은 key의 Pool ENI만 선택; AWSMachine 값 우선 |
| `eni.dcn.ssu.ac.kr/allocation-result` | `AWSMachine` | ENI 주입 성공 시 `Allocated` 기록 |

사용자는 첫 번째 annotation을 `AWSMachineTemplate.spec.template.metadata`에
설정합니다. CAPI가 이를 생성되는 `AWSMachine.metadata`에 복사하므로 Webhook은
별도의 템플릿 조회 없이 요청 객체만 검사할 수 있습니다.

## 4. ENIPool API

`ENIPool`은 cluster-scoped 리소스로 설계합니다. ENI와 VPC가 Kubernetes
namespace에 귀속되지 않기 때문입니다. 특정 namespace의 사용을 제한해야 한다면
향후 namespace selector 또는 별도 정책 필드를 추가합니다.

```yaml
apiVersion: networking.dcn.ssu.ac.kr/v1alpha1
kind: ENIPool
metadata:
  name: prod-ap-northeast-2
spec:
  region: ap-northeast-2
  vpcID: vpc-0123456789abcdef0
  exhaustionPolicy: Dynamic
  interfaces:
    - id: eni-00000000000000001
      privateIP: 10.20.1.25
    - id: eni-00000000000000002
      privateIP: 10.20.1.26
status:
  observedGeneration: 1
  conditions: []
  interfaces:
    - id: eni-00000000000000001
      privateIP: 10.20.1.25
      state: Available
    - id: eni-00000000000000002
      privateIP: 10.20.1.26
      state: Claimed
      claimRef:
        namespace: workload-cluster
        name: worker-abcde
```

### 4.1 Spec 필드

| 필드 | 필수 | 설명 |
| --- | --- | --- |
| `region` | 예 | AWS Region |
| `vpcID` | 예 | Pool이 속한 VPC ID |
| `interfaces` | 예 | ENI ID와 기대하는 primary private IPv4 목록 |
| `exhaustionPolicy` | 아니오 | 기본값 `Dynamic`; `Dynamic`, `Wait`, `Fail` 지원 예정 |

`availabilityZone`과 `subnetID`는 spec 필드가 아닙니다. 현재 trust mode에서는
AWS 조회를 하지 않으므로 status의 해당 값도 채우지 않습니다.

### 4.2 유효성 검사

Admission Webhook은 선언 데이터에 대해 다음을 검증합니다.

- ENI ID 형식
- private IPv4 형식
- 동일 Pool 내부의 ENI ID 및 IP 중복
- 동일 ENI가 다른 Pool에 중복 등록되지 않았는지

AWS 측 ENI, VPC, IP 일치 여부는 검증하지 않고 선언이 정확하다고 가정합니다.

## 5. ENIClaim API

`ENIClaim`은 Machine별 예약과 회수를 나타내는 cluster-scoped 리소스입니다.
ENI ID를 리소스 이름으로 사용하므로 서로 다른 namespace의 Machine 사이에서도
Kubernetes API의 원자적 `CREATE`로 중복 할당을 방지할 수 있습니다.

```yaml
apiVersion: networking.dcn.ssu.ac.kr/v1alpha1
kind: ENIClaim
metadata:
  name: eni-00000000000000001
spec:
  machineRef:
    namespace: workload-cluster
    name: worker-abcde
  poolRef:
    name: prod-ap-northeast-2
  eniID: eni-00000000000000001
status:
  phase: Bound
  privateIP: 10.20.1.25
```

`ENIClaim`에는 finalizer를 설정합니다. AWS API를 조회하지 않으므로 소유
`AWSMachine`이 Kubernetes API에서 완전히 사라진 뒤 Claim을 삭제합니다.

## 6. Pool 선택

Mutating Webhook은 `AWSMachine`의 cluster-name label로 소속 CAPI `Cluster`를 찾고,
`Cluster.spec.infrastructureRef`가 가리키는 `AWSCluster`에서 다음 값을 구합니다.

- Region
- 실제 VPC ID

CAPA가 기존 VPC를 사용하면 선언된 ID를 사용합니다. CREATE admission에서 Region
또는 VPC ID를 확인할 수 없으면 opt-in 요청을 실패시켜 잘못된 ENI로 진행하지
않습니다.

선택 조건은 다음과 같습니다.

```text
ENIPool.spec.region == AWSCluster의 Region
AND
ENIPool.spec.vpcID == AWSCluster의 실제 VPC ID
```

Pool spec에는 AZ/subnet 조건을 두지 않습니다. Webhook이 선택한 ENI의 기존
subnet과 AZ가 인스턴스 네트워크 배치를 결정하며, CAPA/AWS가 해당 조건으로 EC2를
생성하도록 `AWSMachine.spec.networkInterfaces`에 ENI를 전달합니다.

상위 CAPI 리소스의 failure domain과 ENI의 실제 AZ가 충돌하면 CAPA/EC2 생성이
실패할 수 있습니다. trust mode Controller는 이를 사전에 검증하지 않습니다.

Region/VPC가 같은 Pool이 여러 개라면 비결정적인 선택을 피하기 위해 향후
priority 또는 selector 정책을 추가해야 합니다. 초기 버전에서는 하나만 존재하도록
Validating Webhook으로 제한합니다.

## 7. Admission Webhook

Webhook 대상은 `AWSMachine`의 `CREATE` 요청입니다.

### 7.1 대상이 아닌 요청

다음 경우 JSON patch 없이 허용합니다.

- `eni.dcn.ssu.ac.kr/allocate-from-pool` annotation이 없거나 `true`가 아닌 경우
- 이미 `spec.networkInterfaces`가 지정된 경우

### 7.2 대상 요청

Webhook은 Cluster와 AWSCluster에서 Region/VPC를 확인하고, 일치하는 Pool의
미사용 ENI를 private IPv4 숫자값 오름차순으로 정렬합니다. ENI ID를 이름으로 한
cluster-scoped `ENIClaim`을 원자적으로 생성한 후 admission 응답의
`AWSMachine.spec.networkInterfaces`에 ENI ID를 주입합니다.

CAPA v2.13은 생성된 `AWSMachine.spec` 변경을 금지하므로 이 작업은 반드시 CREATE
admission 안에서 끝나야 합니다. Webhook은 AWS API를 호출하지 않습니다. dry-run
요청에서는 Claim을 만들지 않아 Kubernetes side effect가 없습니다.

기존에 사용자가 설정한 `cluster.x-k8s.io/paused` annotation이 있다면 allocator는
개입하지 않습니다.

## 8. Allocator Controller

정상 할당의 선택·Claim·spec 주입은 Mutating Webhook이 담당합니다. Controller는
다음 후속 상태를 관리합니다.

1. ENIClaim이 참조하는 Pool과 AWSMachine 존재 여부 확인
2. Claim status에 `Bound`와 Pool에 선언된 private IP 기록
3. Claim 목록을 기준으로 ENIPool inventory status 갱신
4. AWSMachine 삭제 완료 후 Claim finalizer 제거 및 ENI 반환

Admission에서 Claim이 AWSMachine 저장보다 먼저 생성되는 짧은 구간을 고려해,
Claim Controller는 새 Claim에 유예시간을 둡니다. Admission 실패로 AWSMachine이
생기지 않은 경우 유예시간 후 orphan Claim을 삭제합니다.

### 8.2 Dynamic fallback

Pool이 없거나 사용 가능한 ENI가 없고 정책이 `Dynamic`이면:

1. `AWSMachine.spec.networkInterfaces`를 비워 둔다.
2. CREATE 요청을 변경하지 않고 허용한다.
3. CAPA가 기존 방식으로 subnet 내 private IP를 자동 할당한다.

fallback은 오류가 아니라 명시적인 정상 상태입니다. 운영자는
`eni_pool_available_interfaces` metric으로 Pool 가용량을 확인할 수 있습니다.

## 9. 동시성

여러 `AWSMachine`이 동시에 생성될 수 있으므로 status 배열을 단순 갱신하는
것만으로는 안전하지 않습니다.

- ENI 하나당 활성 `ENIClaim`은 최대 하나여야 합니다.
- cluster-scoped Claim 이름에 ENI ID를 사용해 생성 충돌을
  Kubernetes API의 원자적 `CREATE`로 해결합니다.
- 충돌한 admission 요청은 다른 ENI를 선택해 재시도합니다.
- Kubernetes status 및 Claim 갱신은 항상 재실행 가능해야 합니다.

## 10. 삭제와 회수

Machine 삭제 시 Claim은 다음 순서로 회수합니다.

1. 삭제 중인 `AWSMachine`이 Kubernetes API에서 완전히 사라질 때까지 Claim을 유지한다.
2. Claim을 삭제하고 finalizer를 제거한다.
3. Pool status에서 해당 ENI를 Available 상태로 반환한다.

ENI 자체는 사람이 관리하는 Pool 자산이므로 Controller가 삭제하지 않습니다.

## 11. 권한

### Kubernetes RBAC

- `AWSMachine`, `AWSCluster`, `Cluster` 읽기
- AWSMachine CREATE admission 응답 patch
- `ENIPool`, `ENIClaim` CRUD 및 status 갱신
- Event 생성

### AWS IAM

이 Controller는 AWS API를 호출하지 않으므로 별도의 AWS IAM 권한이 필요하지
않습니다. CAPA가 사전 생성 ENI를 인스턴스에 연결하는 데 필요한 권한은 기존
CAPA controller IAM role에 있어야 합니다.

## 12. 구현 구성

1. Kubebuilder 프로젝트와 `ENIPool`, `ENIClaim` API 생성
2. CRD validation 및 defaulting 구현
3. opt-in CREATE Mutating Webhook 구현
4. AWSCluster Region/VPC resolver 구현
5. 선언 및 Claim 기반 Pool inventory reconciler 구현
6. Admission Claim 기반 allocator 구현
7. Dynamic fallback 구현
8. 삭제/finalizer 처리 구현
9. envtest 동시성 및 장애 복구 테스트
