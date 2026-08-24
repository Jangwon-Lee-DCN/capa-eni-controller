# KPT installation package

이 디렉터리는 CAPA ENI Controller의 CRD, RBAC, Webhook, cert-manager 인증서와
Deployment를 하나의 YAML로 렌더링한 KPT 패키지입니다.

`install.yaml`의 기본 이미지는 다음과 같습니다.

```text
jangwonlee/capa-eni-controller:v0.2.0
```

직접 빌드한 이미지를 사용할 때는 프로젝트 루트에서 패키지를 다시 생성하십시오.

```bash
make docker-build docker-push IMG=<registry>/capa-eni-controller:<tag>
make build-installer IMG=<registry>/capa-eni-controller:<tag>
cp dist/install.yaml package/install.yaml
```

적용 전 cert-manager가 management cluster에 설치되어 있어야 합니다.

```bash
kpt live init package
kpt live apply package --reconcile-timeout=5m
```

Controller 자체는 AWS API를 호출하지 않으므로 별도의 AWS IAM 자격 증명이
필요하지 않습니다. ENI 연결 권한은 기존 CAPA controller에 있어야 합니다.
