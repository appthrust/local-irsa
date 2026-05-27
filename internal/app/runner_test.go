package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const testIssuerID = "7f3k6q2m"

func TestSafeName(t *testing.T) {
	tests := map[string]string{
		"DNS_API_DEV": "dns-api-dev",
		"---":         "cluster",
		"prod.1":      "prod-1",
	}
	for input, want := range tests {
		if got := safeName(input); got != want {
			t.Fatalf("safeName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestInitCreatesStateAndSnippetOnly(t *testing.T) {
	root := t.TempDir()
	runner := testRunner(root)
	var stdout bytes.Buffer
	err := runner.Execute(context.Background(), []string{
		"init",
		"--name", "DNS_API_DEV",
		"--profile", "pg",
	}, strings.NewReader(""), &stdout, io.Discard)
	if err != nil {
		t.Fatalf("init failed: %v", err)
	}

	stateDir := filepath.Join(root, "clusters", "DNS_API_DEV")
	stateBytes, err := os.ReadFile(filepath.Join(stateDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state State
	if err := json.Unmarshal(stateBytes, &state); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(stateBytes), "initializedAt") {
		t.Fatalf("state contains initializedAt: %s", stateBytes)
	}
	if state.IssuerID != testIssuerID {
		t.Fatalf("issuerID = %q", state.IssuerID)
	}
	if state.Bucket != "local-irsa-667640692788-ap-northeast-1-dns-api-dev-"+testIssuerID {
		t.Fatalf("bucket = %q", state.Bucket)
	}
	if state.Profile != "pg" {
		t.Fatalf("profile = %q", state.Profile)
	}
	snippet, err := os.ReadFile(filepath.Join(stateDir, "kind-irsa-snippet.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(snippet)
	for _, want := range []string{"service-account-issuer", "service-account-jwks-uri"} {
		if !strings.Contains(text, want) {
			t.Fatalf("snippet does not contain %s:\n%s", want, text)
		}
	}
	for _, forbidden := range []string{"service-account-signing-key-file", "service-account-key-file"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("snippet contains forbidden %s:\n%s", forbidden, text)
		}
	}
	for _, forbiddenFile := range []string{"sa.key", "sa.pub", "openid-configuration.json", "keys.json"} {
		if _, err := os.Stat(filepath.Join(stateDir, forbiddenFile)); err == nil {
			t.Fatalf("init created forbidden file %s", forbiddenFile)
		}
	}
}

func TestInitRerunDoesNotChangeStateJSON(t *testing.T) {
	root := t.TempDir()
	runner := testRunner(root)
	args := []string{"init", "--name", "dev", "--profile", "pg"}
	if err := runner.Execute(context.Background(), args, strings.NewReader(""), io.Discard, io.Discard); err != nil {
		t.Fatalf("first init failed: %v", err)
	}
	runner.IssuerIDGenerator = func() (string, error) {
		return "", errors.New("issuerID generator should not be called for existing state")
	}
	statePath := filepath.Join(root, "clusters", "dev", "state.json")
	first, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Execute(context.Background(), args, strings.NewReader(""), io.Discard, io.Discard); err != nil {
		t.Fatalf("second init failed: %v", err)
	}
	second, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("state changed after rerun:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestInitWithBucketOmitsIssuerID(t *testing.T) {
	root := t.TempDir()
	runner := testRunner(root)
	if err := runner.Execute(context.Background(), []string{"init", "--name", "dev", "--bucket", "custom-issuer-bucket"}, strings.NewReader(""), io.Discard, io.Discard); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	stateBytes, err := os.ReadFile(filepath.Join(root, "clusters", "dev", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state State
	if err := json.Unmarshal(stateBytes, &state); err != nil {
		t.Fatal(err)
	}
	if state.IssuerID != "" {
		t.Fatalf("issuerID = %q, want empty", state.IssuerID)
	}
	if state.Bucket != "custom-issuer-bucket" {
		t.Fatalf("bucket = %q", state.Bucket)
	}
}

func TestInitRejectsClusterLevelStateMismatch(t *testing.T) {
	root := t.TempDir()
	runner := testRunner(root)
	var stdout bytes.Buffer
	if err := runner.Execute(context.Background(), []string{"init", "--name", "dev"}, strings.NewReader(""), &stdout, io.Discard); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	err := runner.Execute(context.Background(), []string{"init", "--name", "dev", "--bucket", "other-bucket"}, strings.NewReader(""), io.Discard, io.Discard)
	if err == nil {
		t.Fatal("expected mismatch error")
	}
}

func TestRootHelpListsUserCommands(t *testing.T) {
	runner := testRunner(t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := runner.Execute(context.Background(), []string{"--help"}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("help failed: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
	text := stdout.String()
	for _, want := range []string{
		"Usage: local-irsa <command>",
		"Commands:",
		"init --name=NAME",
		"install --name=NAME",
		"bind --name=NAME",
		"unbind --name=NAME",
		"doctor --name=NAME",
		"down --name=NAME",
		"demo create-policy --name=NAME",
		"demo run --name=NAME",
		"demo delete-policy --name=NAME",
		`Run "local-irsa <command> --help" for more information on a command.`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("root help does not contain %q:\n%s", want, text)
		}
	}
	for _, forbidden := range []string{"task up", "task down", "LOCAL_IRSA_DEV"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("root help contains developer-only text %q:\n%s", forbidden, text)
		}
	}
}

func TestSubcommandHelpIncludesDetailFlagsAndExample(t *testing.T) {
	runner := testRunner(t.TempDir())
	var stdout bytes.Buffer
	if err := runner.Execute(context.Background(), []string{"init", "--help"}, strings.NewReader(""), &stdout, io.Discard); err != nil {
		t.Fatalf("init help failed: %v", err)
	}
	text := stdout.String()
	for _, want := range []string{
		"Create local-irsa state for one cluster",
		"Example:",
		"local-irsa init --name dev --region ap-northeast-1",
		"--name=NAME",
		"--region=REGION",
		"--bucket=BUCKET",
		"--profile=PROFILE",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("init help does not contain %q:\n%s", want, text)
		}
	}
	for _, forbidden := range []string{"task up", "task down", "LOCAL_IRSA_DEV"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("init help contains developer-only text %q:\n%s", forbidden, text)
		}
	}
}

func TestInitProgressAndFinalOutput(t *testing.T) {
	root := t.TempDir()
	runner := testRunner(root)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runner.Execute(context.Background(), []string{"init", "--name", "dev"}, strings.NewReader(""), &stdout, &stderr)
	if err != nil {
		t.Fatalf("init failed: %v", err)
	}
	statePath := filepath.Join(root, "clusters", "dev", "state.json")
	snippetPath := filepath.Join(root, "clusters", "dev", "kind-irsa-snippet.yaml")
	issuerURL := "https://local-irsa-667640692788-ap-northeast-1-dev-" + testIssuerID + ".s3.ap-northeast-1.amazonaws.com"
	progress := stderr.String()
	for _, want := range []string{
		"→ Resolve AWS settings\n",
		"✓ Resolve AWS settings  region ap-northeast-1\n",
		"→ Get AWS account ID\n",
		"✓ Get AWS account ID  667640692788\n",
		"→ Decide issuer URL\n",
		"✓ Decide issuer URL  " + issuerURL + "\n",
		"→ Write state\n",
		"✓ Write state  " + statePath + "\n",
		"→ Write kind snippet\n",
		"✓ Write kind snippet  " + snippetPath + "\n",
	} {
		if !strings.Contains(progress, want) {
			t.Fatalf("progress does not contain %q:\n%s", want, progress)
		}
	}
	result := stdout.String()
	for _, want := range []string{
		"State:\n  path: " + statePath,
		"Kind snippet:\n  path: " + snippetPath,
		"Issuer:\n  url: " + issuerURL,
		"Next:\n  1. Merge the kind snippet into kind.yaml.",
		"  3. Run local-irsa install --name dev.",
	} {
		if !strings.Contains(result, want) {
			t.Fatalf("stdout does not contain %q:\n%s", want, result)
		}
	}
}

func TestQuietSuppressesSuccessfulProgress(t *testing.T) {
	runner := testRunner(t.TempDir())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runner.Execute(context.Background(), []string{"--quiet", "init", "--name", "dev"}, strings.NewReader(""), &stdout, &stderr)
	if err != nil {
		t.Fatalf("quiet init failed: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if !strings.Contains(stdout.String(), "State:\n  path:") {
		t.Fatalf("quiet stdout missing final result:\n%s", stdout.String())
	}
}

func TestVerboseShowsProgressDetails(t *testing.T) {
	runner := testRunner(t.TempDir())
	var stderr bytes.Buffer
	err := runner.Execute(context.Background(), []string{"--verbose", "init", "--name", "dev"}, strings.NewReader(""), io.Discard, &stderr)
	if err != nil {
		t.Fatalf("verbose init failed: %v", err)
	}
	if !strings.Contains(stderr.String(), "ℹ S3 bucket  local-irsa-667640692788-ap-northeast-1-dev-"+testIssuerID+"\n") {
		t.Fatalf("stderr missing verbose detail:\n%s", stderr.String())
	}
}

func TestQuietAndVerboseConflict(t *testing.T) {
	runner := testRunner(t.TempDir())
	var stderr bytes.Buffer
	err := runner.Execute(context.Background(), []string{"--quiet", "--verbose", "init", "--name", "dev"}, strings.NewReader(""), io.Discard, &stderr)
	if err == nil {
		t.Fatal("expected quiet and verbose conflict")
	}
	if !strings.Contains(stderr.String(), "--quiet and --verbose cannot be used together") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestInstallSkipWebhookOmitsWebhookStep(t *testing.T) {
	root := t.TempDir()
	runner := testRunner(root)
	if err := runner.Execute(context.Background(), []string{"init", "--name", "dev"}, strings.NewReader(""), io.Discard, io.Discard); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runner.Execute(context.Background(), []string{"install", "--name", "dev", "--skip-webhook"}, strings.NewReader(""), &stdout, &stderr)
	if err != nil {
		t.Fatalf("install failed: %v", err)
	}
	progress := stderr.String()
	for _, want := range []string{
		"→ Load state\n",
		"→ Check cluster OIDC\n",
		"→ Read cluster JWKS\n",
		"→ Prepare S3 issuer\n",
		"→ Publish issuer documents\n",
		"→ Ensure IAM OIDC Provider\n",
	} {
		if !strings.Contains(progress, want) {
			t.Fatalf("install progress does not contain %q:\n%s", want, progress)
		}
	}
	if strings.Contains(progress, "Apply webhook") {
		t.Fatalf("install progress contains skipped webhook step:\n%s", progress)
	}
	if strings.Contains(progress, "Check webhook prerequisites") {
		t.Fatalf("install progress contains skipped webhook prerequisite step:\n%s", progress)
	}
	if strings.Contains(progress, "Check webhook readiness") {
		t.Fatalf("install progress contains skipped webhook readiness step:\n%s", progress)
	}
	if strings.Contains(progress, "Check webhook mutation") {
		t.Fatalf("install progress contains skipped webhook mutation step:\n%s", progress)
	}
	if !strings.Contains(stdout.String(), "Webhook:\n  status: skipped") {
		t.Fatalf("install stdout missing webhook status:\n%s", stdout.String())
	}
}

func TestInstallAppliesWebhookChecksReadinessAndMutation(t *testing.T) {
	root := t.TempDir()
	runner := testRunner(root)
	if err := runner.Execute(context.Background(), []string{"init", "--name", "dev"}, strings.NewReader(""), io.Discard, io.Discard); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	kube := &recordingInstallKube{}
	runner.KubectlFactory = func(string) KubeClient { return kube }
	var stderr bytes.Buffer
	err := runner.Execute(context.Background(), []string{"install", "--name", "dev"}, strings.NewReader(""), io.Discard, &stderr)
	if err != nil {
		t.Fatalf("install failed: %v", err)
	}
	wantCalls := "apply,wait:local-irsa-system/pod-identity-webhook/120s,can-i:local-irsa-system/pod-identity-webhook:get/list/watch:serviceaccounts,mutation:667640692788/ap-northeast-1"
	if strings.Join(kube.calls, ",") != wantCalls {
		t.Fatalf("kube calls = %v", kube.calls)
	}
	progress := stderr.String()
	for _, want := range []string{
		"→ Apply webhook\n",
		"✓ Apply webhook  applied\n",
		"→ Check webhook readiness\n",
		"✓ Check webhook readiness  deployment/pod-identity-webhook\n",
		"→ Check webhook mutation\n",
		"✓ Check webhook mutation  server-side dry-run\n",
	} {
		if !strings.Contains(progress, want) {
			t.Fatalf("progress missing %q:\n%s", want, progress)
		}
	}
}

func TestInstallFailsWhenWebhookReadinessFails(t *testing.T) {
	root := t.TempDir()
	runner := testRunner(root)
	if err := runner.Execute(context.Background(), []string{"init", "--name", "dev"}, strings.NewReader(""), io.Discard, io.Discard); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	runner.KubectlFactory = func(string) KubeClient {
		return &recordingInstallKube{readinessErr: errors.New("deployment not available\npods:\nfailed pod\nlogs:\nfailed log")}
	}
	var stderr bytes.Buffer
	err := runner.Execute(context.Background(), []string{"install", "--name", "dev"}, strings.NewReader(""), io.Discard, &stderr)
	if err == nil {
		t.Fatal("expected install to fail")
	}
	for _, want := range []string{"✗ Check webhook readiness", "deployment not available", "pods:", "logs:"} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr missing %q:\n%s", want, stderr.String())
		}
	}
}

func TestInstallFailsWhenWebhookRBACFails(t *testing.T) {
	root := t.TempDir()
	runner := testRunner(root)
	if err := runner.Execute(context.Background(), []string{"init", "--name", "dev"}, strings.NewReader(""), io.Discard, io.Discard); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	runner.KubectlFactory = func(string) KubeClient {
		return &recordingInstallKube{rbacErr: errors.New("pod-identity-webhook cannot list serviceaccounts")}
	}
	var stderr bytes.Buffer
	err := runner.Execute(context.Background(), []string{"install", "--name", "dev"}, strings.NewReader(""), io.Discard, &stderr)
	if err == nil {
		t.Fatal("expected install to fail")
	}
	for _, want := range []string{"✗ Check webhook readiness", "cannot list serviceaccounts"} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr missing %q:\n%s", want, stderr.String())
		}
	}
}

func TestInstallFailsWhenWebhookMutationFails(t *testing.T) {
	root := t.TempDir()
	runner := testRunner(root)
	if err := runner.Execute(context.Background(), []string{"init", "--name", "dev"}, strings.NewReader(""), io.Discard, io.Discard); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	runner.KubectlFactory = func(string) KubeClient {
		return &recordingInstallKube{mutationErr: errors.New("webhook smoke pod missing AWS_ROLE_ARN\nlogs:\nmutation failed")}
	}
	var stderr bytes.Buffer
	err := runner.Execute(context.Background(), []string{"install", "--name", "dev"}, strings.NewReader(""), io.Discard, &stderr)
	if err == nil {
		t.Fatal("expected install to fail")
	}
	for _, want := range []string{"✗ Check webhook mutation", "missing AWS_ROLE_ARN", "logs:"} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr missing %q:\n%s", want, stderr.String())
		}
	}
}

func TestValidateWebhookSmokePodAcceptsMountedProjectedToken(t *testing.T) {
	raw := []byte(`{
  "spec": {
    "containers": [
      {
        "name": "smoke",
        "env": [
          {"name": "AWS_REGION", "value": "ap-northeast-1"},
          {"name": "AWS_ROLE_ARN", "value": "arn:aws:iam::667640692788:role/local-irsa-webhook-smoke"},
          {"name": "AWS_WEB_IDENTITY_TOKEN_FILE", "value": "/var/run/secrets/eks.amazonaws.com/serviceaccount/token"}
        ],
        "volumeMounts": [
          {"name": "aws-iam-token", "mountPath": "/var/run/secrets/eks.amazonaws.com/serviceaccount"}
        ]
      }
    ],
    "volumes": [
      {
        "name": "aws-iam-token",
        "projected": {
          "sources": [
            {
              "serviceAccountToken": {
                "audience": "sts.amazonaws.com",
                "path": "token"
              }
            }
          ]
        }
      }
    ]
  }
}`)
	err := validateWebhookSmokePod(raw, "arn:aws:iam::667640692788:role/local-irsa-webhook-smoke")
	if err != nil {
		t.Fatalf("validateWebhookSmokePod failed: %v", err)
	}
}

func TestInstallChecksWebhookPrerequisitesBeforeAWSChanges(t *testing.T) {
	root := t.TempDir()
	awsClient := &fakeAWS{region: "ap-northeast-1", accountID: "667640692788"}
	runner := testRunnerWithAWS(root, awsClient)
	if err := runner.Execute(context.Background(), []string{"init", "--name", "dev"}, strings.NewReader(""), io.Discard, io.Discard); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	runner.KubectlFactory = func(string) KubeClient { return certManagerMissingKube{} }
	var stderr bytes.Buffer
	err := runner.Execute(context.Background(), []string{"install", "--name", "dev"}, strings.NewReader(""), io.Discard, &stderr)
	if err == nil {
		t.Fatal("expected install to fail without cert-manager")
	}
	progress := stderr.String()
	if !strings.Contains(progress, "→ Check webhook prerequisites\n") {
		t.Fatalf("install progress missing prerequisite step:\n%s", progress)
	}
	if !strings.Contains(progress, "✗ Check webhook prerequisites") {
		t.Fatalf("install progress missing prerequisite failure:\n%s", progress)
	}
	if awsClient.ensureIssuerCalls != 0 {
		t.Fatalf("EnsureIssuer calls = %d, want 0", awsClient.ensureIssuerCalls)
	}
	if awsClient.ensureOIDCProviderCalls != 0 {
		t.Fatalf("EnsureOIDCProvider calls = %d, want 0", awsClient.ensureOIDCProviderCalls)
	}
}

func TestWebhookManifestUsesExplicitCommand(t *testing.T) {
	manifest := webhookManifest()
	for _, want := range []string{
		"kind: ClusterRole\nmetadata:\n  name: pod-identity-webhook.local-irsa.appthrust.io\n",
		"    resources:\n      - serviceaccounts\n",
		"    verbs:\n      - get\n      - list\n      - watch\n",
		"kind: ClusterRoleBinding\nmetadata:\n  name: pod-identity-webhook.local-irsa.appthrust.io\n",
		"    name: pod-identity-webhook\n    namespace: local-irsa-system\n",
		"    admissionReviewVersions:\n      - v1beta1\n",
		"          command:\n            - /webhook\n",
		"            - --annotation-prefix=eks.amazonaws.com\n",
		"            - --token-audience=sts.amazonaws.com\n",
		"            - --in-cluster=false\n",
		"            - --namespace=local-irsa-system\n",
		"            - --service-name=pod-identity-webhook\n",
		"            - --port=8443\n",
		"            - --tls-cert=/etc/webhook/certs/tls.crt\n",
		"            - --tls-key=/etc/webhook/certs/tls.key\n",
	} {
		if !strings.Contains(manifest, want) {
			t.Fatalf("manifest missing %q:\n%s", want, manifest)
		}
	}
	if strings.Contains(manifest, "          args:\n") {
		t.Fatalf("manifest still uses args:\n%s", manifest)
	}
	if strings.Contains(manifest, "--in-cluster=true") {
		t.Fatalf("manifest still uses in-cluster=true:\n%s", manifest)
	}
}

func TestDoctorWithoutServiceAccountOmitsServiceAccountSteps(t *testing.T) {
	root := t.TempDir()
	runner := testRunner(root)
	if err := runner.Execute(context.Background(), []string{"init", "--name", "dev"}, strings.NewReader(""), io.Discard, io.Discard); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	var stderr bytes.Buffer
	err := runner.Execute(context.Background(), []string{"doctor", "--name", "dev"}, strings.NewReader(""), io.Discard, &stderr)
	if err != nil {
		t.Fatalf("doctor failed: %v", err)
	}
	progress := stderr.String()
	if strings.Contains(progress, "Check ServiceAccount binding") || strings.Contains(progress, "Test web identity") {
		t.Fatalf("doctor progress contains ServiceAccount steps:\n%s", progress)
	}
}

func TestDoctorPrintsStateReport(t *testing.T) {
	root := t.TempDir()
	runner := testRunner(root)
	if err := runner.Execute(context.Background(), []string{"init", "--name", "dev", "--profile", "example"}, strings.NewReader(""), io.Discard, io.Discard); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runner.Execute(context.Background(), []string{"doctor", "--name", "dev"}, strings.NewReader(""), &stdout, &stderr)
	if err != nil {
		t.Fatalf("doctor failed: %v", err)
	}

	statePath := filepath.Join(root, "clusters", "dev", "state.json")
	for _, want := range []string{
		"State:\n",
		"  path: " + statePath + "\n",
		"  json:\n",
		"    {\n",
		"      \"name\": \"dev\",\n",
		"      \"region\": \"ap-northeast-1\",\n",
		"      \"profile\": \"example\"\n",
		"Doctor:\n",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout missing %q:\n%s", want, stdout.String())
		}
	}
	if strings.Contains(stdout.String(), "\033[") {
		t.Fatalf("non-TTY state report contains ANSI color:\n%q", stdout.String())
	}
	if strings.Index(stdout.String(), "State:") > strings.Index(stdout.String(), "Doctor:") {
		t.Fatalf("State block appears after Doctor block:\n%s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "✓ Load state  "+statePath+"\n") {
		t.Fatalf("stderr missing Load state success:\n%s", stderr.String())
	}
}

func TestDoctorQuietOmitsStateReport(t *testing.T) {
	root := t.TempDir()
	runner := testRunner(root)
	if err := runner.Execute(context.Background(), []string{"init", "--name", "dev"}, strings.NewReader(""), io.Discard, io.Discard); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	var stdout bytes.Buffer
	err := runner.Execute(context.Background(), []string{"--quiet", "doctor", "--name", "dev"}, strings.NewReader(""), &stdout, io.Discard)
	if err != nil {
		t.Fatalf("doctor failed: %v", err)
	}
	if strings.Contains(stdout.String(), "State:") {
		t.Fatalf("quiet doctor printed State block:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "Doctor:") {
		t.Fatalf("quiet doctor missing final result:\n%s", stdout.String())
	}
}

func TestHighlightJSON(t *testing.T) {
	got := highlightJSON("{\"name\":\"dev\",\"count\":123,\"ok\":true,\"empty\":null}")
	for _, want := range []string{
		colorCyan + "\"name\"" + colorReset,
		colorGreen + "\"dev\"" + colorReset,
		colorMagenta + "123" + colorReset,
		colorYellow + "true" + colorReset,
		colorDim + "null" + colorReset,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("highlighted JSON missing %q:\n%q", want, got)
		}
	}
}

func TestDownYesWithoutDeleteBucketOmitsConfirmAndBucketSteps(t *testing.T) {
	root := t.TempDir()
	runner := testRunner(root)
	if err := runner.Execute(context.Background(), []string{"init", "--name", "dev"}, strings.NewReader(""), io.Discard, io.Discard); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	var stderr bytes.Buffer
	err := runner.Execute(context.Background(), []string{"down", "--name", "dev", "--yes"}, strings.NewReader(""), io.Discard, &stderr)
	if err != nil {
		t.Fatalf("down failed: %v", err)
	}
	progress := stderr.String()
	for _, forbidden := range []string{"Confirm deletion", "Delete S3 bucket"} {
		if strings.Contains(progress, forbidden) {
			t.Fatalf("down progress contains %q:\n%s", forbidden, progress)
		}
	}
}

func TestDownWithoutDeleteBucketKeepsStateAndShowsNextStepEvenQuiet(t *testing.T) {
	root := t.TempDir()
	runner := testRunner(root)
	if err := runner.Execute(context.Background(), []string{"init", "--name", "dev"}, strings.NewReader(""), io.Discard, io.Discard); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	var stderr bytes.Buffer
	err := runner.Execute(context.Background(), []string{"--quiet", "down", "--name", "dev", "--yes"}, strings.NewReader(""), io.Discard, &stderr)
	if err != nil {
		t.Fatalf("down failed: %v", err)
	}
	statePath := filepath.Join(root, "clusters", "dev", "state.json")
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("state should remain after down without --delete-bucket: %v", err)
	}
	bucket := "local-irsa-667640692788-ap-northeast-1-dev-" + testIssuerID
	for _, want := range []string{
		"ℹ S3 bucket kept  s3://" + bucket,
		"ℹ Delete bucket  local-irsa down --name dev --delete-bucket",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr missing %q:\n%s", want, stderr.String())
		}
	}
	if strings.Contains(stderr.String(), "✓ Delete S3 issuer objects") {
		t.Fatalf("quiet stderr contains successful progress:\n%s", stderr.String())
	}
}

func TestDownPromptIsNotAProgressStep(t *testing.T) {
	runner := testRunner(t.TempDir())
	var stdout, stderr bytes.Buffer
	err := runner.Execute(context.Background(), []string{"down", "--name", "dev"}, strings.NewReader("n\n"), &stdout, &stderr)
	if err != nil {
		t.Fatalf("down failed: %v", err)
	}
	if !strings.Contains(stdout.String(), "Delete local-irsa resources for dev?\n  Type y to continue: ") {
		t.Fatalf("down stdout missing confirmation prompt:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "Aborted") {
		t.Fatalf("down stdout missing abort message:\n%s", stdout.String())
	}
	if strings.Contains(stderr.String(), "Confirm deletion") {
		t.Fatalf("down progress contains confirmation prompt:\n%s", stderr.String())
	}
}

func TestDownDeleteBucketRequiresDeletedProvider(t *testing.T) {
	root := t.TempDir()
	awsClient := &fakeAWS{region: "ap-northeast-1", accountID: "667640692788", cleanupProviderDeleted: false}
	runner := testRunnerWithAWS(root, awsClient)
	if err := runner.Execute(context.Background(), []string{"init", "--name", "dev"}, strings.NewReader(""), io.Discard, io.Discard); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	var stderr bytes.Buffer
	err := runner.Execute(context.Background(), []string{"down", "--name", "dev", "--yes", "--delete-bucket"}, strings.NewReader(""), io.Discard, &stderr)
	if err != nil {
		t.Fatalf("down failed: %v", err)
	}
	if awsClient.cleanupIssuerDeleteBucketCalls != 0 {
		t.Fatalf("CleanupIssuer deleteBucket calls = %d, want 0", awsClient.cleanupIssuerDeleteBucketCalls)
	}
	if _, err := os.Stat(filepath.Join(root, "clusters", "dev", "state.json")); err != nil {
		t.Fatalf("state should remain when provider remains: %v", err)
	}
	if !strings.Contains(stderr.String(), "skipped because IAM OIDC Provider remains") {
		t.Fatalf("stderr missing skipped bucket deletion warning:\n%s", stderr.String())
	}
}

func TestDownDeleteBucketRemovesStateAfterFullCleanup(t *testing.T) {
	root := t.TempDir()
	runner := testRunner(root)
	if err := runner.Execute(context.Background(), []string{"init", "--name", "dev"}, strings.NewReader(""), io.Discard, io.Discard); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	err := runner.Execute(context.Background(), []string{"down", "--name", "dev", "--yes", "--delete-bucket"}, strings.NewReader(""), io.Discard, io.Discard)
	if err != nil {
		t.Fatalf("down failed: %v", err)
	}
	stateDir := filepath.Join(root, "clusters", "dev")
	if _, err := os.Stat(stateDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("state directory exists after full cleanup, err=%v", err)
	}
}

func TestDownRemovesDeletedBindingsFromState(t *testing.T) {
	root := t.TempDir()
	awsClient := &fakeAWS{
		region:                 "ap-northeast-1",
		accountID:              "667640692788",
		cleanupProviderDeleted: true,
		cleanupRoleDeletedByName: map[string]bool{
			"role-a": true,
			"role-b": false,
		},
	}
	runner := testRunnerWithAWS(root, awsClient)
	if err := runner.Execute(context.Background(), []string{"init", "--name", "dev"}, strings.NewReader(""), io.Discard, io.Discard); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	for _, roleName := range []string{"role-a", "role-b"} {
		err := runner.Execute(context.Background(), []string{
			"bind",
			"--name", "dev",
			"--namespace", "default",
			"--service-account", strings.TrimPrefix(roleName, "role-"),
			"--role-name", roleName,
			"--policy-arn", "arn:aws:iam::aws:policy/AmazonS3ReadOnlyAccess",
		}, strings.NewReader(""), io.Discard, io.Discard)
		if err != nil {
			t.Fatalf("bind %s failed: %v", roleName, err)
		}
	}
	if err := runner.Execute(context.Background(), []string{"down", "--name", "dev", "--yes"}, strings.NewReader(""), io.Discard, io.Discard); err != nil {
		t.Fatalf("down failed: %v", err)
	}
	state := mustLoadTestState(t, filepath.Join(root, "clusters", "dev", "state.json"))
	if len(state.Bindings) != 1 || state.Bindings[0].RoleName != "role-b" {
		t.Fatalf("bindings after down = %+v, want only role-b", state.Bindings)
	}
}

func TestDownKeepsStateAndRemovesDeletedBindingsOnRoleError(t *testing.T) {
	root := t.TempDir()
	awsClient := &fakeAWS{
		region:                 "ap-northeast-1",
		accountID:              "667640692788",
		cleanupProviderDeleted: true,
		cleanupRoleDeletedByName: map[string]bool{
			"role-a": true,
		},
		cleanupRoleErrors: map[string]error{
			"role-b": errors.New("delete role failed"),
		},
	}
	runner := testRunnerWithAWS(root, awsClient)
	if err := runner.Execute(context.Background(), []string{"init", "--name", "dev"}, strings.NewReader(""), io.Discard, io.Discard); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	for _, roleName := range []string{"role-a", "role-b", "role-c"} {
		err := runner.Execute(context.Background(), []string{
			"bind",
			"--name", "dev",
			"--namespace", "default",
			"--service-account", strings.TrimPrefix(roleName, "role-"),
			"--role-name", roleName,
			"--policy-arn", "arn:aws:iam::aws:policy/AmazonS3ReadOnlyAccess",
		}, strings.NewReader(""), io.Discard, io.Discard)
		if err != nil {
			t.Fatalf("bind %s failed: %v", roleName, err)
		}
	}
	err := runner.Execute(context.Background(), []string{"down", "--name", "dev", "--yes", "--delete-bucket"}, strings.NewReader(""), io.Discard, io.Discard)
	if err == nil {
		t.Fatal("expected down to fail")
	}
	state := mustLoadTestState(t, filepath.Join(root, "clusters", "dev", "state.json"))
	gotRoles := make([]string, 0, len(state.Bindings))
	for _, binding := range state.Bindings {
		gotRoles = append(gotRoles, binding.RoleName)
	}
	if strings.Join(gotRoles, ",") != "role-b,role-c" {
		t.Fatalf("roles after failed down = %v, want role-b,role-c", gotRoles)
	}
}

func TestDoctorRequiresNamespaceAndServiceAccountTogether(t *testing.T) {
	runner := testRunner(t.TempDir())
	var stderr bytes.Buffer
	err := runner.Execute(context.Background(), []string{"doctor", "--name", "dev", "--namespace", "default"}, strings.NewReader(""), io.Discard, &stderr)
	if err == nil {
		t.Fatal("expected doctor validation error")
	}
	if !strings.Contains(err.Error(), "--namespace and --service-account must be specified together") {
		t.Fatalf("error = %v", err)
	}
	if !strings.Contains(stderr.String(), "local-irsa: --namespace and --service-account must be specified together") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestBindAcceptsRepeatedPolicyARNs(t *testing.T) {
	root := t.TempDir()
	runner := testRunner(root)
	if err := runner.Execute(context.Background(), []string{"init", "--name", "dev"}, strings.NewReader(""), io.Discard, io.Discard); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	err := runner.Execute(context.Background(), []string{
		"bind",
		"--name", "dev",
		"--namespace", "default",
		"--service-account", "app",
		"--role-name", "app-dev",
		"--policy-arn", "arn:aws:iam::aws:policy/AmazonS3ReadOnlyAccess",
		"--policy-arn", "arn:aws:iam::aws:policy/AmazonDynamoDBReadOnlyAccess",
	}, strings.NewReader(""), io.Discard, io.Discard)
	if err != nil {
		t.Fatalf("bind failed: %v", err)
	}

	stateBytes, err := os.ReadFile(filepath.Join(root, "clusters", "dev", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state State
	if err := json.Unmarshal(stateBytes, &state); err != nil {
		t.Fatal(err)
	}
	if len(state.Bindings) != 1 {
		t.Fatalf("bindings = %+v", state.Bindings)
	}
	got := state.Bindings[0].PolicyARNs
	want := []string{
		"arn:aws:iam::aws:policy/AmazonS3ReadOnlyAccess",
		"arn:aws:iam::aws:policy/AmazonDynamoDBReadOnlyAccess",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("policy ARNs = %v, want %v", got, want)
	}
}

func TestUnbindRemovesOneBindingRoleAndAnnotations(t *testing.T) {
	root := t.TempDir()
	awsClient := &fakeAWS{region: "ap-northeast-1", accountID: "667640692788"}
	kube := &recordingUnbindKube{exists: true}
	runner := testRunnerWithAWS(root, awsClient)
	runner.KubectlFactory = func(string) KubeClient { return kube }

	if err := runner.Execute(context.Background(), []string{"init", "--name", "dev"}, strings.NewReader(""), io.Discard, io.Discard); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	for _, item := range []struct {
		serviceAccount string
		roleName       string
	}{
		{serviceAccount: "app", roleName: "app-dev"},
		{serviceAccount: "keep", roleName: "keep-dev"},
	} {
		err := runner.Execute(context.Background(), []string{
			"bind",
			"--name", "dev",
			"--namespace", "default",
			"--service-account", item.serviceAccount,
			"--role-name", item.roleName,
			"--policy-arn", "arn:aws:iam::aws:policy/AmazonS3ReadOnlyAccess",
		}, strings.NewReader(""), io.Discard, io.Discard)
		if err != nil {
			t.Fatalf("bind %s failed: %v", item.roleName, err)
		}
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runner.Execute(context.Background(), []string{
		"unbind",
		"--name", "dev",
		"--namespace", "default",
		"--service-account", "app",
	}, strings.NewReader(""), &stdout, &stderr)
	if err != nil {
		t.Fatalf("unbind failed: %v", err)
	}

	state := mustLoadTestState(t, filepath.Join(root, "clusters", "dev", "state.json"))
	if len(state.Bindings) != 1 || state.Bindings[0].ServiceAccount != "keep" {
		t.Fatalf("bindings after unbind = %+v, want only keep", state.Bindings)
	}
	if len(awsClient.cleanupRoleBindings) != 1 || awsClient.cleanupRoleBindings[0].RoleName != "app-dev" {
		t.Fatalf("cleanup role bindings = %+v", awsClient.cleanupRoleBindings)
	}
	if strings.Join(kube.removedAnnotations, "\n") != "default/app:eks.amazonaws.com/audience,eks.amazonaws.com/role-arn,eks.amazonaws.com/sts-regional-endpoints,eks.amazonaws.com/token-expiration" {
		t.Fatalf("removed annotations = %v", kube.removedAnnotations)
	}
	for _, want := range []string{
		"→ Load state\n",
		"→ Find binding\n",
		"→ Remove ServiceAccount annotations\n",
		"→ Detach managed policies\n",
		"→ Delete IAM Role\n",
		"→ Save binding\n",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("progress missing %q:\n%s", want, stderr.String())
		}
	}
	for _, want := range []string{"Unbound:", "serviceAccount: default/app", "role: app-dev"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestUnbindFailsWhenBindingMissing(t *testing.T) {
	root := t.TempDir()
	runner := testRunner(root)
	if err := runner.Execute(context.Background(), []string{"init", "--name", "dev"}, strings.NewReader(""), io.Discard, io.Discard); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	var stderr bytes.Buffer
	err := runner.Execute(context.Background(), []string{
		"unbind",
		"--name", "dev",
		"--namespace", "default",
		"--service-account", "app",
	}, strings.NewReader(""), io.Discard, &stderr)
	if err == nil {
		t.Fatal("expected unbind to fail")
	}
	if !strings.Contains(err.Error(), "binding default/app does not exist in state") {
		t.Fatalf("error = %v", err)
	}
	if !strings.Contains(stderr.String(), "✗ Find binding") {
		t.Fatalf("stderr missing Find binding failure:\n%s", stderr.String())
	}
}

func TestUnbindKeepsBindingWhenRoleIsNotOwned(t *testing.T) {
	root := t.TempDir()
	awsClient := &fakeAWS{
		region:    "ap-northeast-1",
		accountID: "667640692788",
		cleanupRoleDeletedByName: map[string]bool{
			"app-dev": false,
		},
	}
	runner := testRunnerWithAWS(root, awsClient)
	if err := runner.Execute(context.Background(), []string{"init", "--name", "dev"}, strings.NewReader(""), io.Discard, io.Discard); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	err := runner.Execute(context.Background(), []string{
		"bind",
		"--name", "dev",
		"--namespace", "default",
		"--service-account", "app",
		"--role-name", "app-dev",
		"--policy-arn", "arn:aws:iam::aws:policy/AmazonS3ReadOnlyAccess",
	}, strings.NewReader(""), io.Discard, io.Discard)
	if err != nil {
		t.Fatalf("bind failed: %v", err)
	}
	err = runner.Execute(context.Background(), []string{
		"unbind",
		"--name", "dev",
		"--namespace", "default",
		"--service-account", "app",
	}, strings.NewReader(""), io.Discard, io.Discard)
	if err == nil {
		t.Fatal("expected unbind to fail")
	}
	if !strings.Contains(err.Error(), "IAM Role app-dev is not owned by local-irsa cluster dev") {
		t.Fatalf("error = %v", err)
	}
	state := mustLoadTestState(t, filepath.Join(root, "clusters", "dev", "state.json"))
	if len(state.Bindings) != 1 || state.Bindings[0].RoleName != "app-dev" {
		t.Fatalf("bindings after failed unbind = %+v", state.Bindings)
	}
}

func TestUnbindSkipsMissingServiceAccountAndRemovesBinding(t *testing.T) {
	root := t.TempDir()
	awsClient := &fakeAWS{region: "ap-northeast-1", accountID: "667640692788"}
	kube := &recordingUnbindKube{exists: false}
	runner := testRunnerWithAWS(root, awsClient)
	runner.KubectlFactory = func(string) KubeClient { return kube }
	if err := runner.Execute(context.Background(), []string{"init", "--name", "dev"}, strings.NewReader(""), io.Discard, io.Discard); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	statePath := filepath.Join(root, "clusters", "dev", "state.json")
	state := mustLoadTestState(t, statePath)
	state.Bindings = []Binding{{
		Namespace:      "default",
		ServiceAccount: "app",
		RoleName:       "app-dev",
		RoleARN:        "arn:aws:iam::667640692788:role/app-dev",
		PolicyARNs:     []string{"arn:aws:iam::aws:policy/AmazonS3ReadOnlyAccess"},
	}}
	if err := saveStateFile(statePath, state); err != nil {
		t.Fatalf("save state failed: %v", err)
	}

	var stderr bytes.Buffer
	err := runner.Execute(context.Background(), []string{
		"unbind",
		"--name", "dev",
		"--namespace", "default",
		"--service-account", "app",
	}, strings.NewReader(""), io.Discard, &stderr)
	if err != nil {
		t.Fatalf("unbind failed: %v", err)
	}
	state = mustLoadTestState(t, statePath)
	if len(state.Bindings) != 0 {
		t.Fatalf("bindings after unbind = %+v, want none", state.Bindings)
	}
	if len(kube.removedAnnotations) != 0 {
		t.Fatalf("removed annotations for missing ServiceAccount = %v", kube.removedAnnotations)
	}
	if !strings.Contains(stderr.String(), "skipped missing default/app") {
		t.Fatalf("stderr missing skipped annotation message:\n%s", stderr.String())
	}
}

func TestDemoCreatePolicyCreatesPolicyAndPrintsBindExample(t *testing.T) {
	root := t.TempDir()
	awsClient := &fakeAWS{region: "ap-northeast-1", accountID: "667640692788", cleanupProviderDeleted: true}
	runner := testRunnerWithAWS(root, awsClient)
	if err := runner.Execute(context.Background(), []string{"init", "--name", "DNS_API_DEV"}, strings.NewReader(""), io.Discard, io.Discard); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runner.Execute(context.Background(), []string{"demo", "create-policy", "--name", "DNS_API_DEV"}, strings.NewReader(""), &stdout, &stderr)
	if err != nil {
		t.Fatalf("demo create-policy failed: %v", err)
	}

	if awsClient.ensureDemoPolicyCalls != 1 {
		t.Fatalf("EnsureDemoPolicy calls = %d, want 1", awsClient.ensureDemoPolicyCalls)
	}
	request := awsClient.lastDemoPolicyRequest
	if request.PolicyName != "local-irsa-dns-api-dev-demo" || request.RoleName != "local-irsa-dns-api-dev-demo" {
		t.Fatalf("demo policy request = %+v", request)
	}
	if request.Tags[demoPurposeTagKey] != demoPurposeTagValue {
		t.Fatalf("demo purpose tag = %q", request.Tags[demoPurposeTagKey])
	}
	if !strings.Contains(string(request.Document), `"Action": "iam:GetRole"`) {
		t.Fatalf("demo policy document missing iam:GetRole:\n%s", request.Document)
	}
	result := stdout.String()
	for _, want := range []string{
		"Demo policy:",
		"status: created",
		"name: local-irsa-dns-api-dev-demo",
		"arn: arn:aws:iam::667640692788:policy/local-irsa-dns-api-dev-demo",
		"local-irsa bind --name DNS_API_DEV --namespace default --service-account local-irsa-demo --role-name local-irsa-dns-api-dev-demo --policy-arn arn:aws:iam::667640692788:policy/local-irsa-dns-api-dev-demo --create-service-account",
	} {
		if !strings.Contains(result, want) {
			t.Fatalf("stdout missing %q:\n%s", want, result)
		}
	}
	progress := stderr.String()
	for _, want := range []string{"→ Load state\n", "→ Resolve AWS account\n", "→ Ensure demo policy\n"} {
		if !strings.Contains(progress, want) {
			t.Fatalf("progress missing %q:\n%s", want, progress)
		}
	}
}

func TestDemoRunExecutesAWSCLIPodAndCleansUp(t *testing.T) {
	root := t.TempDir()
	runner := testRunner(root)
	if err := runner.Execute(context.Background(), []string{"init", "--name", "dev"}, strings.NewReader(""), io.Discard, io.Discard); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	kube := &recordingDemoKube{roleARN: "arn:aws:iam::667640692788:role/local-irsa-dev-demo"}
	runner.KubectlFactory = func(contextName string) KubeClient {
		kube.contextName = contextName
		return kube
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := runner.Execute(context.Background(), []string{"demo", "run", "--name", "dev", "--context", "kind-dev"}, strings.NewReader(""), &stdout, &stderr)
	if err != nil {
		t.Fatalf("demo run failed: %v", err)
	}

	if kube.contextName != "kind-dev" {
		t.Fatalf("contextName = %q", kube.contextName)
	}
	if kube.runCalls != 1 || kube.deleteCalls != 0 {
		t.Fatalf("run/delete calls = %d/%d, want 1/0", kube.runCalls, kube.deleteCalls)
	}
	if kube.runRoleName != "local-irsa-dev-demo" {
		t.Fatalf("run role name = %q", kube.runRoleName)
	}
	result := stdout.String()
	for _, want := range []string{
		"Command:\n  kubectl \\\n    --context kind-dev \\\n    -n default \\\n    run local-irsa-demo-dev",
		"--image public.ecr.aws/aws-cli/aws-cli:latest",
		"--env AWS_REGION=ap-northeast-1",
		"--attach=true",
		"--rm=true",
		"--pod-running-timeout=120s",
		"--command -- \\\n    sh -c 'set -eu\nset -x",
		"aws sts get-caller-identity",
		"aws iam get-role --role-name local-irsa-dev-demo --query Role.Arn --output text'",
		"Output:\n  + test -n arn:aws:iam::667640692788:role/local-irsa-dev-demo",
		"Demo:",
		"assumed role: arn:aws:sts::667640692788:assumed-role/local-irsa-dev-demo/local-irsa-demo",
		"role: arn:aws:iam::667640692788:role/local-irsa-dev-demo",
	} {
		if !strings.Contains(result, want) {
			t.Fatalf("stdout missing %q:\n%s", want, result)
		}
	}
	if strings.Contains(result, "secret-token") {
		t.Fatalf("stdout contains token contents:\n%s", result)
	}
	if strings.Contains(result, "sleep") {
		t.Fatalf("stdout contains sleep command:\n%s", result)
	}
	progress := stderr.String()
	for _, want := range []string{
		"→ Check ServiceAccount\n",
		"→ Run AWS CLI pod\n",
		"→ Check injected environment\n",
		"→ Check AWS identity\n",
		"→ Check IAM Role\n",
		"→ Clean up demo pod\n",
	} {
		if !strings.Contains(progress, want) {
			t.Fatalf("progress missing %q:\n%s", want, progress)
		}
	}
}

func TestDemoRunCleansUpPodAfterAWSIdentityFailure(t *testing.T) {
	root := t.TempDir()
	runner := testRunner(root)
	if err := runner.Execute(context.Background(), []string{"init", "--name", "dev"}, strings.NewReader(""), io.Discard, io.Discard); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	kube := &recordingDemoKube{
		roleARN:           "arn:aws:iam::667640692788:role/local-irsa-dev-demo",
		output:            "+ test -n arn:aws:iam::667640692788:role/local-irsa-dev-demo\n+ test -f /var/run/secrets/eks.amazonaws.com/serviceaccount/token\n",
		podExistsAfterRun: true,
	}
	runner.KubectlFactory = func(string) KubeClient { return kube }

	var stderr bytes.Buffer
	err := runner.Execute(context.Background(), []string{"demo", "run", "--name", "dev"}, strings.NewReader(""), io.Discard, &stderr)
	if err == nil {
		t.Fatal("expected demo run to fail")
	}
	if kube.deleteCalls != 1 {
		t.Fatalf("DeletePod calls = %d, want 1", kube.deleteCalls)
	}
	if !strings.Contains(stderr.String(), "→ Clean up demo pod\n") {
		t.Fatalf("progress missing cleanup step:\n%s", stderr.String())
	}
}

func TestDemoDeletePolicyDeletesDemoPolicy(t *testing.T) {
	root := t.TempDir()
	awsClient := &fakeAWS{
		region:                  "ap-northeast-1",
		accountID:               "667640692788",
		cleanupProviderDeleted:  true,
		deleteDemoPolicyDeleted: true,
	}
	runner := testRunnerWithAWS(root, awsClient)
	if err := runner.Execute(context.Background(), []string{"init", "--name", "dev"}, strings.NewReader(""), io.Discard, io.Discard); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	var stdout bytes.Buffer
	err := runner.Execute(context.Background(), []string{"demo", "delete-policy", "--name", "dev"}, strings.NewReader(""), &stdout, io.Discard)
	if err != nil {
		t.Fatalf("demo delete-policy failed: %v", err)
	}
	if awsClient.deleteDemoPolicyCalls != 1 {
		t.Fatalf("DeleteDemoPolicy calls = %d, want 1", awsClient.deleteDemoPolicyCalls)
	}
	if awsClient.lastDemoPolicyRequest.PolicyName != "local-irsa-dev-demo" {
		t.Fatalf("policy name = %q", awsClient.lastDemoPolicyRequest.PolicyName)
	}
	if !strings.Contains(stdout.String(), "status: deleted") {
		t.Fatalf("stdout missing deleted status:\n%s", stdout.String())
	}
}

func TestValidateJWKS(t *testing.T) {
	if keyCount, err := validateJWKS([]byte(`{"keys":[{"kid":"one"}]}`)); err != nil {
		t.Fatalf("valid JWKS rejected: %v", err)
	} else if keyCount != 1 {
		t.Fatalf("key count = %d, want 1", keyCount)
	}
	if _, err := validateJWKS([]byte(`{"keys":[]}`)); err == nil {
		t.Fatal("empty JWKS accepted")
	}
	if _, err := validateJWKS([]byte(`not json`)); err == nil {
		t.Fatal("invalid JSON accepted")
	}
}

func TestUpsertBindingSortsAndReplaces(t *testing.T) {
	got := upsertBinding([]Binding{
		{Namespace: "z", ServiceAccount: "b", RoleName: "old"},
		{Namespace: "a", ServiceAccount: "b", RoleName: "keep"},
	}, Binding{Namespace: "z", ServiceAccount: "b", RoleName: "new"})
	if len(got) != 2 {
		t.Fatalf("len = %d", len(got))
	}
	if got[0].Namespace != "a" || got[0].RoleName != "keep" {
		t.Fatalf("first binding = %+v", got[0])
	}
	if got[1].Namespace != "z" || got[1].RoleName != "new" {
		t.Fatalf("second binding = %+v", got[1])
	}
}

func TestTrustPolicyLimitsSubjectAndAudience(t *testing.T) {
	state := State{
		IssuerURL: "https://bucket.s3.ap-northeast-1.amazonaws.com",
	}
	binding := Binding{Namespace: "default", ServiceAccount: "app"}
	raw, err := trustPolicy(state, "arn:aws:iam::123456789012:oidc-provider/bucket.s3.ap-northeast-1.amazonaws.com", binding)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{
		"bucket.s3.ap-northeast-1.amazonaws.com:aud",
		"bucket.s3.ap-northeast-1.amazonaws.com:sub",
		"sts.amazonaws.com",
		"system:serviceaccount:default:app",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("trust policy does not contain %q: %s", want, text)
		}
	}
}

func TestGenerateIssuerIDUsesBucketSafeBase32(t *testing.T) {
	issuerID, err := generateIssuerID()
	if err != nil {
		t.Fatalf("generateIssuerID failed: %v", err)
	}
	if err := validateIssuerID(issuerID); err != nil {
		t.Fatalf("generated issuerID is invalid: %v", err)
	}
}

func testRunner(root string) *Runner {
	return testRunnerWithAWS(root, &fakeAWS{region: "ap-northeast-1", accountID: "667640692788", cleanupProviderDeleted: true})
}

func mustLoadTestState(t *testing.T, path string) State {
	t.Helper()
	stateBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var state State
	if err := json.Unmarshal(stateBytes, &state); err != nil {
		t.Fatal(err)
	}
	return state
}

func testRunnerWithAWS(root string, awsClient *fakeAWS) *Runner {
	return &Runner{
		AWSFactory: fakeAWSFactory{
			client: awsClient,
		},
		KubectlFactory: func(string) KubeClient { return fakeKube{} },
		IssuerIDGenerator: func() (string, error) {
			return testIssuerID, nil
		},
		Env: func(key string) string {
			if key == "LOCAL_IRSA_STATE_ROOT" {
				return root
			}
			return ""
		},
		HomeDir: func() (string, error) { return root, nil },
	}
}

type fakeAWSFactory struct {
	client AWSClient
}

func (f fakeAWSFactory) New(context.Context, AWSOptions) (AWSClient, error) {
	return f.client, nil
}

type fakeAWS struct {
	region                         string
	accountID                      string
	ensureIssuerCalls              int
	ensureOIDCProviderCalls        int
	ensureDemoPolicyCalls          int
	deleteDemoPolicyCalls          int
	deleteDemoPolicyDeleted        bool
	lastDemoPolicyRequest          DemoPolicyRequest
	cleanupProviderDeleted         bool
	cleanupRoleDeletedByName       map[string]bool
	cleanupRoleErrors              map[string]error
	cleanupRoleBindings            []Binding
	cleanupIssuerDeleteBucketCalls int
}

func (f *fakeAWS) Region() string { return f.region }
func (f *fakeAWS) AccountID(context.Context) (string, error) {
	return f.accountID, nil
}
func (f *fakeAWS) EnsureIssuer(context.Context, State, []byte, []byte) error {
	f.ensureIssuerCalls++
	return nil
}
func (f *fakeAWS) CheckIssuerObjects(context.Context, State, []byte, []byte) error {
	return nil
}
func (f *fakeAWS) EnsureOIDCProvider(context.Context, State) (string, error) {
	f.ensureOIDCProviderCalls++
	return "arn:aws:iam::667640692788:oidc-provider/example", nil
}
func (f *fakeAWS) CheckOIDCProvider(context.Context, State) error { return nil }
func (f *fakeAWS) EnsureRole(context.Context, State, Binding) (string, error) {
	return "arn:aws:iam::667640692788:role/example", nil
}
func (f *fakeAWS) EnsureDemoPolicy(_ context.Context, state State, request DemoPolicyRequest) (DemoPolicyDetails, error) {
	f.ensureDemoPolicyCalls++
	f.lastDemoPolicyRequest = request
	return DemoPolicyDetails{
		Status:           "created",
		PolicyName:       request.PolicyName,
		PolicyARN:        customerManagedPolicyARN(state, request.PolicyName),
		AccountID:        state.AccountID,
		RoleName:         request.RoleName,
		DefaultVersionID: "v1",
		Document:         request.Document,
		Tags:             request.Tags,
	}, nil
}
func (f *fakeAWS) AssumeRoleWithWebIdentity(context.Context, string, string, int32) (string, error) {
	return "arn:aws:sts::667640692788:assumed-role/example/local-irsa-doctor", nil
}
func (f *fakeAWS) DeleteDemoPolicy(_ context.Context, _ State, request DemoPolicyRequest) (bool, error) {
	f.deleteDemoPolicyCalls++
	f.lastDemoPolicyRequest = request
	return f.deleteDemoPolicyDeleted, nil
}
func (f *fakeAWS) CleanupRole(_ context.Context, _ State, binding Binding) (bool, error) {
	f.cleanupRoleBindings = append(f.cleanupRoleBindings, binding)
	if err := f.cleanupRoleErrors[binding.RoleName]; err != nil {
		return false, err
	}
	if f.cleanupRoleDeletedByName != nil {
		return f.cleanupRoleDeletedByName[binding.RoleName], nil
	}
	return true, nil
}
func (f *fakeAWS) CleanupProvider(context.Context, State) (bool, error) {
	return f.cleanupProviderDeleted, nil
}
func (f *fakeAWS) CleanupIssuer(_ context.Context, _ State, deleteBucket bool) (bool, error) {
	if deleteBucket {
		f.cleanupIssuerDeleteBucketCalls++
	}
	return true, nil
}

type fakeKube struct{}

type recordingUnbindKube struct {
	fakeKube
	exists             bool
	removedAnnotations []string
}

type certManagerMissingKube struct {
	fakeKube
}

type recordingInstallKube struct {
	fakeKube
	calls        []string
	readinessErr error
	rbacErr      error
	mutationErr  error
}

func (certManagerMissingKube) CheckCertManager(context.Context) error {
	return errors.New("cert-manager Certificate CRD is required")
}

func (fakeKube) Raw(_ context.Context, path string) ([]byte, error) {
	const issuerURL = "https://local-irsa-667640692788-ap-northeast-1-dev-" + testIssuerID + ".s3.ap-northeast-1.amazonaws.com"
	switch path {
	case "/.well-known/openid-configuration":
		return []byte(`{"issuer":"` + issuerURL + `","jwks_uri":"` + issuerURL + `/keys.json"}`), nil
	case "/openid/v1/jwks":
		return []byte(`{"keys":[{"kid":"one"}]}`), nil
	default:
		return nil, nil
	}
}
func (fakeKube) CheckCertManager(context.Context) error { return nil }
func (fakeKube) Apply(context.Context, string) error    { return nil }
func (fakeKube) WaitDeploymentAvailable(context.Context, string, string, string) error {
	return nil
}
func (fakeKube) CheckServiceAccountPermissions(context.Context, string, string, []string, string) error {
	return nil
}
func (fakeKube) CheckWebhookMutation(context.Context, string, string) error {
	return nil
}
func (fakeKube) ServiceAccountExists(context.Context, string, string) (bool, error) {
	return true, nil
}
func (fakeKube) CreateServiceAccount(context.Context, string, string) error { return nil }
func (fakeKube) AnnotateServiceAccount(context.Context, string, string, map[string]string) error {
	return nil
}
func (fakeKube) RemoveServiceAccountAnnotations(context.Context, string, string, []string) error {
	return nil
}

func (k *recordingUnbindKube) ServiceAccountExists(context.Context, string, string) (bool, error) {
	return k.exists, nil
}

func (k *recordingUnbindKube) RemoveServiceAccountAnnotations(_ context.Context, namespace, name string, annotations []string) error {
	annotations = append([]string(nil), annotations...)
	sort.Strings(annotations)
	k.removedAnnotations = append(k.removedAnnotations, namespace+"/"+name+":"+strings.Join(annotations, ","))
	return nil
}
func (fakeKube) ServiceAccountRoleARN(context.Context, string, string) (string, error) {
	return "arn:aws:iam::667640692788:role/example", nil
}
func (fakeKube) CreateToken(context.Context, string, string, string, string) (string, error) {
	return "token", nil
}
func (fakeKube) PodExists(context.Context, string, string) (bool, error) {
	return false, nil
}
func (fakeKube) RunDemoPod(context.Context, string, string, string, string, string) ([]byte, error) {
	return []byte(demoRunTestLogs), nil
}
func (fakeKube) DeletePod(context.Context, string, string) error {
	return nil
}

func (k *recordingInstallKube) Apply(context.Context, string) error {
	k.calls = append(k.calls, "apply")
	return nil
}

func (k *recordingInstallKube) WaitDeploymentAvailable(_ context.Context, namespace, name, timeout string) error {
	k.calls = append(k.calls, "wait:"+namespace+"/"+name+"/"+timeout)
	return k.readinessErr
}

func (k *recordingInstallKube) CheckServiceAccountPermissions(_ context.Context, namespace, name string, verbs []string, resource string) error {
	k.calls = append(k.calls, "can-i:"+namespace+"/"+name+":"+strings.Join(verbs, "/")+":"+resource)
	return k.rbacErr
}

func (k *recordingInstallKube) CheckWebhookMutation(_ context.Context, accountID, region string) error {
	k.calls = append(k.calls, "mutation:"+accountID+"/"+region)
	return k.mutationErr
}

type recordingDemoKube struct {
	roleARN           string
	contextName       string
	runRoleName       string
	runCalls          int
	runErr            error
	output            string
	podExistsCalls    int
	podExistsAfterRun bool
	deleteCalls       int
}

func (k *recordingDemoKube) Raw(ctx context.Context, path string) ([]byte, error) {
	return fakeKube{}.Raw(ctx, path)
}
func (k *recordingDemoKube) CheckCertManager(context.Context) error { return nil }
func (k *recordingDemoKube) Apply(context.Context, string) error    { return nil }
func (k *recordingDemoKube) WaitDeploymentAvailable(context.Context, string, string, string) error {
	return nil
}
func (k *recordingDemoKube) CheckServiceAccountPermissions(context.Context, string, string, []string, string) error {
	return nil
}
func (k *recordingDemoKube) CheckWebhookMutation(context.Context, string, string) error {
	return nil
}
func (k *recordingDemoKube) ServiceAccountExists(context.Context, string, string) (bool, error) {
	return true, nil
}
func (k *recordingDemoKube) CreateServiceAccount(context.Context, string, string) error { return nil }
func (k *recordingDemoKube) AnnotateServiceAccount(context.Context, string, string, map[string]string) error {
	return nil
}
func (k *recordingDemoKube) RemoveServiceAccountAnnotations(context.Context, string, string, []string) error {
	return nil
}
func (k *recordingDemoKube) ServiceAccountRoleARN(context.Context, string, string) (string, error) {
	return k.roleARN, nil
}
func (k *recordingDemoKube) CreateToken(context.Context, string, string, string, string) (string, error) {
	return "token", nil
}
func (k *recordingDemoKube) PodExists(context.Context, string, string) (bool, error) {
	k.podExistsCalls++
	if k.podExistsCalls == 1 {
		return false, nil
	}
	return k.podExistsAfterRun, nil
}
func (k *recordingDemoKube) RunDemoPod(_ context.Context, _, _, _, _, roleName string) ([]byte, error) {
	k.runCalls++
	k.runRoleName = roleName
	if k.output != "" {
		return []byte(k.output), k.runErr
	}
	return []byte(demoRunTestLogs), k.runErr
}
func (k *recordingDemoKube) DeletePod(context.Context, string, string) error {
	k.deleteCalls++
	return nil
}

const demoRunTestLogs = `+ test -n arn:aws:iam::667640692788:role/local-irsa-dev-demo
+ test -n /var/run/secrets/eks.amazonaws.com/serviceaccount/token
+ test -f /var/run/secrets/eks.amazonaws.com/serviceaccount/token
+ aws sts get-caller-identity
{
    "UserId": "AROAXAMPLE:local-irsa-demo",
    "Account": "667640692788",
    "Arn": "arn:aws:sts::667640692788:assumed-role/local-irsa-dev-demo/local-irsa-demo"
}
+ aws iam get-role --role-name local-irsa-dev-demo --query Role.Arn --output text
arn:aws:iam::667640692788:role/local-irsa-dev-demo
`
