package app

func webhookManifest() string {
	return `apiVersion: v1
kind: Namespace
metadata:
  name: local-irsa-system
---
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: pod-identity-webhook-selfsigned
  namespace: local-irsa-system
spec:
  selfSigned: {}
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: pod-identity-webhook
  namespace: local-irsa-system
spec:
  secretName: pod-identity-webhook-cert
  dnsNames:
    - pod-identity-webhook.local-irsa-system.svc
    - pod-identity-webhook.local-irsa-system.svc.cluster.local
  issuerRef:
    name: pod-identity-webhook-selfsigned
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: pod-identity-webhook
  namespace: local-irsa-system
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: pod-identity-webhook.local-irsa.appthrust.io
rules:
  - apiGroups:
      - ""
    resources:
      - serviceaccounts
    verbs:
      - get
      - list
      - watch
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: pod-identity-webhook.local-irsa.appthrust.io
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: pod-identity-webhook.local-irsa.appthrust.io
subjects:
  - kind: ServiceAccount
    name: pod-identity-webhook
    namespace: local-irsa-system
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: pod-identity-webhook
  namespace: local-irsa-system
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: pod-identity-webhook
  template:
    metadata:
      labels:
        app.kubernetes.io/name: pod-identity-webhook
    spec:
      serviceAccountName: pod-identity-webhook
      containers:
        - name: webhook
          image: public.ecr.aws/eks/amazon-eks-pod-identity-webhook:v0.6.15
          command:
            - /webhook
            - --annotation-prefix=eks.amazonaws.com
            - --token-audience=sts.amazonaws.com
            - --in-cluster=false
            - --namespace=local-irsa-system
            - --service-name=pod-identity-webhook
            - --port=8443
            - --tls-cert=/etc/webhook/certs/tls.crt
            - --tls-key=/etc/webhook/certs/tls.key
          ports:
            - name: https
              containerPort: 8443
          volumeMounts:
            - name: certs
              mountPath: /etc/webhook/certs
              readOnly: true
      volumes:
        - name: certs
          secret:
            secretName: pod-identity-webhook-cert
---
apiVersion: v1
kind: Service
metadata:
  name: pod-identity-webhook
  namespace: local-irsa-system
spec:
  selector:
    app.kubernetes.io/name: pod-identity-webhook
  ports:
    - name: https
      port: 443
      targetPort: https
---
apiVersion: admissionregistration.k8s.io/v1
kind: MutatingWebhookConfiguration
metadata:
  name: pod-identity-webhook.local-irsa.appthrust.io
  annotations:
    cert-manager.io/inject-ca-from: local-irsa-system/pod-identity-webhook
webhooks:
  - name: pod-identity-webhook.local-irsa.appthrust.io
    admissionReviewVersions:
      - v1beta1
    sideEffects: None
    failurePolicy: Ignore
    clientConfig:
      service:
        name: pod-identity-webhook
        namespace: local-irsa-system
        path: /mutate
        port: 443
    rules:
      - apiGroups:
          - ""
        apiVersions:
          - v1
        operations:
          - CREATE
        resources:
          - pods
`
}
