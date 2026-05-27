package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"
)

type KubeClient interface {
	Raw(context.Context, string) ([]byte, error)
	CheckCertManager(context.Context) error
	Apply(context.Context, string) error
	WaitDeploymentAvailable(context.Context, string, string, string) error
	CheckServiceAccountPermissions(context.Context, string, string, []string, string) error
	CheckWebhookMutation(context.Context, string, string) error
	ServiceAccountExists(context.Context, string, string) (bool, error)
	CreateServiceAccount(context.Context, string, string) error
	AnnotateServiceAccount(context.Context, string, string, map[string]string) error
	RemoveServiceAccountAnnotations(context.Context, string, string, []string) error
	ServiceAccountRoleARN(context.Context, string, string) (string, error)
	CreateToken(context.Context, string, string, string, string) (string, error)
	PodExists(context.Context, string, string) (bool, error)
	RunDemoPod(context.Context, string, string, string, string, string) ([]byte, error)
	DeletePod(context.Context, string, string) error
}

type Kubectl struct {
	ContextName string
}

func (k Kubectl) Raw(ctx context.Context, path string) ([]byte, error) {
	args := k.baseArgs("get", "--raw", path)
	return runCommand(ctx, "", "kubectl", args...)
}

func (k Kubectl) CheckCertManager(ctx context.Context) error {
	args := k.baseArgs("get", "crd", "certificates.cert-manager.io")
	_, err := runCommand(ctx, "", "kubectl", args...)
	if err != nil {
		return fmt.Errorf("cert-manager Certificate CRD is required: %w", err)
	}
	return nil
}

func (k Kubectl) Apply(ctx context.Context, manifest string) error {
	args := k.baseArgs("apply", "-f", "-")
	_, err := runCommand(ctx, manifest, "kubectl", args...)
	return err
}

func (k Kubectl) WaitDeploymentAvailable(ctx context.Context, namespace, name, timeout string) error {
	args := k.baseArgs("-n", namespace, "wait", "--for=condition=Available", "deployment/"+name, "--timeout="+timeout)
	_, err := runCommand(ctx, "", "kubectl", args...)
	if err == nil {
		return nil
	}
	diagnostics := k.deploymentDiagnostics(ctx, namespace, name)
	if diagnostics == "" {
		return err
	}
	return fmt.Errorf("%w\n%s", err, diagnostics)
}

func (k Kubectl) CheckServiceAccountPermissions(ctx context.Context, namespace, name string, verbs []string, resource string) error {
	asUser := fmt.Sprintf("system:serviceaccount:%s:%s", namespace, name)
	for _, verb := range verbs {
		args := k.baseArgs("auth", "can-i", verb, resource, "--all-namespaces", "--as="+asUser)
		out, err := runCommand(ctx, "", "kubectl", args...)
		if err != nil {
			return err
		}
		if strings.TrimSpace(string(out)) != "yes" {
			return fmt.Errorf("%s cannot %s %s at cluster scope", asUser, verb, resource)
		}
	}
	return nil
}

func (k Kubectl) CheckWebhookMutation(ctx context.Context, accountID, region string) error {
	const namespace = "local-irsa-system"
	const name = "local-irsa-webhook-smoke"
	roleARN := fmt.Sprintf("arn:aws:iam::%s:role/%s", accountID, name)
	if err := k.applyWebhookSmokeServiceAccount(ctx, namespace, name, roleARN); err != nil {
		return err
	}
	defer func() {
		_ = k.deleteServiceAccount(ctx, namespace, name)
	}()
	deadline := time.Now().Add(30 * time.Second)
	var lastErr error
	for {
		podRaw, err := k.serverSideDryRunWebhookSmokePod(ctx, namespace, name, region)
		if err != nil {
			lastErr = err
		} else if err := validateWebhookSmokePod(podRaw, roleARN); err != nil {
			lastErr = err
		} else {
			return nil
		}
		if time.Now().After(deadline) {
			diagnostics := k.deploymentDiagnostics(ctx, "local-irsa-system", "pod-identity-webhook")
			if diagnostics == "" {
				return lastErr
			}
			return fmt.Errorf("%w\n%s", lastErr, diagnostics)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func (k Kubectl) ServiceAccountExists(ctx context.Context, namespace, name string) (bool, error) {
	args := k.baseArgs("-n", namespace, "get", "serviceaccount", name, "-o", "name")
	_, err := runCommand(ctx, "", "kubectl", args...)
	if err == nil {
		return true, nil
	}
	if strings.Contains(err.Error(), "NotFound") || strings.Contains(err.Error(), "not found") {
		return false, nil
	}
	return false, err
}

func (k Kubectl) CreateServiceAccount(ctx context.Context, namespace, name string) error {
	args := k.baseArgs("-n", namespace, "create", "serviceaccount", name)
	_, err := runCommand(ctx, "", "kubectl", args...)
	return err
}

func (k Kubectl) AnnotateServiceAccount(ctx context.Context, namespace, name string, annotations map[string]string) error {
	keys := make([]string, 0, len(annotations))
	for key := range annotations {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	args := k.baseArgs("-n", namespace, "annotate", "serviceaccount", name)
	for _, key := range keys {
		args = append(args, key+"="+annotations[key])
	}
	args = append(args, "--overwrite")
	_, err := runCommand(ctx, "", "kubectl", args...)
	return err
}

func (k Kubectl) RemoveServiceAccountAnnotations(ctx context.Context, namespace, name string, annotations []string) error {
	sort.Strings(annotations)
	args := k.baseArgs("-n", namespace, "annotate", "serviceaccount", name)
	for _, key := range annotations {
		args = append(args, key+"-")
	}
	args = append(args, "--overwrite")
	_, err := runCommand(ctx, "", "kubectl", args...)
	return err
}

func (k Kubectl) ServiceAccountRoleARN(ctx context.Context, namespace, name string) (string, error) {
	args := k.baseArgs("-n", namespace, "get", "serviceaccount", name, "-o", "json")
	out, err := runCommand(ctx, "", "kubectl", args...)
	if err != nil {
		return "", err
	}
	var doc struct {
		Metadata struct {
			Annotations map[string]string `json:"annotations"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		return "", err
	}
	roleARN := doc.Metadata.Annotations["eks.amazonaws.com/role-arn"]
	if roleARN == "" {
		return "", errors.New("service account has no eks.amazonaws.com/role-arn annotation")
	}
	return roleARN, nil
}

func (k Kubectl) CreateToken(ctx context.Context, namespace, name, audience, duration string) (string, error) {
	args := k.baseArgs("-n", namespace, "create", "token", name, "--audience", audience, "--duration", duration)
	out, err := runCommand(ctx, "", "kubectl", args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (k Kubectl) PodExists(ctx context.Context, namespace, name string) (bool, error) {
	args := k.baseArgs("-n", namespace, "get", "pod", name, "-o", "name")
	_, err := runCommand(ctx, "", "kubectl", args...)
	if err == nil {
		return true, nil
	}
	if strings.Contains(err.Error(), "NotFound") || strings.Contains(err.Error(), "not found") {
		return false, nil
	}
	return false, err
}

func (k Kubectl) RunDemoPod(ctx context.Context, namespace, name, serviceAccount, region, roleName string) ([]byte, error) {
	args, err := demoPodRunArgs(k.ContextName, namespace, name, serviceAccount, region, roleName)
	if err != nil {
		return nil, err
	}
	return k.runDemoPodAndMonitorImagePull(ctx, namespace, name, args)
}

func (k Kubectl) DeletePod(ctx context.Context, namespace, name string) error {
	args := k.baseArgs("-n", namespace, "delete", "pod", name, "--ignore-not-found=true", "--wait=false")
	_, err := runCommand(ctx, "", "kubectl", args...)
	return err
}

func (k Kubectl) applyWebhookSmokeServiceAccount(ctx context.Context, namespace, name, roleARN string) error {
	manifest := fmt.Sprintf(`apiVersion: v1
kind: ServiceAccount
metadata:
  name: %s
  namespace: %s
  annotations:
    eks.amazonaws.com/role-arn: "%s"
    eks.amazonaws.com/audience: "%s"
    eks.amazonaws.com/sts-regional-endpoints: "true"
    eks.amazonaws.com/token-expiration: "%s"
`, name, namespace, roleARN, defaultTokenAudience, defaultTokenExpiration)
	args := k.baseArgs("apply", "-f", "-")
	_, err := runCommand(ctx, manifest, "kubectl", args...)
	return err
}

func (k Kubectl) deleteServiceAccount(ctx context.Context, namespace, name string) error {
	args := k.baseArgs("-n", namespace, "delete", "serviceaccount", name, "--ignore-not-found=true")
	_, err := runCommand(ctx, "", "kubectl", args...)
	return err
}

func (k Kubectl) serverSideDryRunWebhookSmokePod(ctx context.Context, namespace, serviceAccount, region string) ([]byte, error) {
	manifest := fmt.Sprintf(`apiVersion: v1
kind: Pod
metadata:
  name: local-irsa-webhook-smoke
  namespace: %s
spec:
  serviceAccountName: %s
  restartPolicy: Never
  containers:
    - name: smoke
      image: public.ecr.aws/aws-cli/aws-cli:latest
      command:
        - "true"
      env:
        - name: AWS_REGION
          value: "%s"
`, namespace, serviceAccount, region)
	args := k.baseArgs("create", "--dry-run=server", "-o", "json", "-f", "-")
	return runCommand(ctx, manifest, "kubectl", args...)
}

func (k Kubectl) deploymentDiagnostics(ctx context.Context, namespace, name string) string {
	var b strings.Builder
	podsArgs := k.baseArgs("-n", namespace, "get", "pods", "-l", "app.kubernetes.io/name="+name, "-o", "wide")
	if out, err := runCommand(ctx, "", "kubectl", podsArgs...); err == nil && len(out) > 0 {
		b.WriteString("pods:\n")
		b.Write(out)
		if !strings.HasSuffix(b.String(), "\n") {
			b.WriteByte('\n')
		}
	} else if err != nil {
		b.WriteString("pods:\n")
		b.WriteString(err.Error())
		b.WriteByte('\n')
	}
	logsArgs := k.baseArgs("-n", namespace, "logs", "deploy/"+name, "--tail=100")
	if out, err := runCommand(ctx, "", "kubectl", logsArgs...); err == nil && len(out) > 0 {
		b.WriteString("logs:\n")
		b.Write(out)
		if !strings.HasSuffix(b.String(), "\n") {
			b.WriteByte('\n')
		}
	} else if err != nil {
		b.WriteString("logs:\n")
		b.WriteString(err.Error())
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

func validateWebhookSmokePod(raw []byte, roleARN string) error {
	var pod struct {
		Spec struct {
			Containers []struct {
				Env []struct {
					Name  string `json:"name"`
					Value string `json:"value"`
				} `json:"env"`
				VolumeMounts []struct {
					Name      string `json:"name"`
					MountPath string `json:"mountPath"`
				} `json:"volumeMounts"`
			} `json:"containers"`
			Volumes []struct {
				Name      string `json:"name"`
				Projected *struct {
					Sources []struct {
						ServiceAccountToken *struct {
							Path     string `json:"path"`
							Audience string `json:"audience"`
						} `json:"serviceAccountToken"`
					} `json:"sources"`
				} `json:"projected"`
			} `json:"volumes"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(raw, &pod); err != nil {
		return fmt.Errorf("webhook smoke pod response is not JSON: %w", err)
	}
	env := map[string]string{}
	volumeMounts := map[string]string{}
	for _, container := range pod.Spec.Containers {
		for _, item := range container.Env {
			env[item.Name] = item.Value
		}
		for _, item := range container.VolumeMounts {
			volumeMounts[item.Name] = item.MountPath
		}
	}
	if env["AWS_ROLE_ARN"] != roleARN {
		return fmt.Errorf("webhook smoke pod missing AWS_ROLE_ARN %s", roleARN)
	}
	if env["AWS_WEB_IDENTITY_TOKEN_FILE"] == "" {
		return errors.New("webhook smoke pod missing AWS_WEB_IDENTITY_TOKEN_FILE")
	}
	if env["AWS_REGION"] == "" && env["AWS_DEFAULT_REGION"] == "" {
		return errors.New("webhook smoke pod missing AWS_REGION or AWS_DEFAULT_REGION")
	}
	tokenPath := env["AWS_WEB_IDENTITY_TOKEN_FILE"]
	for _, volume := range pod.Spec.Volumes {
		if volume.Projected == nil {
			continue
		}
		for _, source := range volume.Projected.Sources {
			if source.ServiceAccountToken == nil {
				continue
			}
			mountPath := volumeMounts[volume.Name]
			if source.ServiceAccountToken.Audience == defaultTokenAudience && mountedTokenPath(mountPath, source.ServiceAccountToken.Path) == tokenPath {
				return nil
			}
		}
	}
	return errors.New("webhook smoke pod missing projected ServiceAccount token volume")
}

func mountedTokenPath(mountPath, tokenPath string) string {
	if mountPath == "" {
		return tokenPath
	}
	return strings.TrimRight(mountPath, "/") + "/" + strings.TrimLeft(tokenPath, "/")
}

func (k Kubectl) baseArgs(args ...string) []string {
	out := []string{}
	if k.ContextName != "" {
		out = append(out, "--context", k.ContextName)
	}
	out = append(out, args...)
	return out
}

func demoPodRunArgs(contextName, namespace, name, serviceAccount, region, roleName string) ([]string, error) {
	overrides, err := demoPodOverrides(serviceAccount)
	if err != nil {
		return nil, err
	}
	script := demoPodScript(roleName)
	args := []string{}
	if contextName != "" {
		args = append(args, "--context", contextName)
	}
	args = append(args,
		"-n", namespace,
		"run", name,
		"--image", demoPodImage,
		"--restart=Never",
		"--overrides", overrides,
		"--env", "AWS_REGION="+region,
		"--env", "AWS_DEFAULT_REGION="+region,
		"--attach=true",
		"--rm=true",
		"--pod-running-timeout=120s",
		"--command", "--",
		"sh", "-c", script,
	)
	return args, nil
}

func demoPodRunCommand(contextName, namespace, name, serviceAccount, region, roleName string) (string, error) {
	overrides, err := demoPodOverrides(serviceAccount)
	if err != nil {
		return "", err
	}
	lines := []string{"  kubectl \\"}
	if contextName != "" {
		lines = append(lines, "    --context "+shellQuote(contextName)+" \\")
	}
	lines = append(lines,
		"    -n "+shellQuote(namespace)+" \\",
		"    run "+shellQuote(name)+" \\",
		"    --image "+shellQuote(demoPodImage)+" \\",
		"    --restart=Never \\",
		"    --overrides "+shellQuote(overrides)+" \\",
		"    --env "+shellQuote("AWS_REGION="+region)+" \\",
		"    --env "+shellQuote("AWS_DEFAULT_REGION="+region)+" \\",
		"    --attach=true \\",
		"    --rm=true \\",
		"    --pod-running-timeout=120s \\",
		"    --command -- \\",
		"    sh -c "+shellQuote(demoPodScript(roleName)),
	)
	return strings.Join(lines, "\n"), nil
}

func demoPodDeleteCommand(contextName, namespace, name string) string {
	args := []string{}
	if contextName != "" {
		args = append(args, "--context", contextName)
	}
	args = append(args, "-n", namespace, "delete", "pod", name, "--ignore-not-found=true")
	return shellQuoteArgs(append([]string{"kubectl"}, args...))
}

func demoPodOverrides(serviceAccount string) (string, error) {
	body, err := json.Marshal(map[string]any{
		"apiVersion": "v1",
		"spec": map[string]any{
			"serviceAccountName": serviceAccount,
		},
	})
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func demoPodScript(roleName string) string {
	return strings.Join([]string{
		"set -eu",
		"set -x",
		`test -n "$AWS_ROLE_ARN"`,
		`test -n "$AWS_WEB_IDENTITY_TOKEN_FILE"`,
		`test -f "$AWS_WEB_IDENTITY_TOKEN_FILE"`,
		"aws sts get-caller-identity",
		"aws iam get-role --role-name " + shellQuote(roleName) + " --query Role.Arn --output text",
	}, "\n")
}

func shellQuoteArgs(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, shellQuote(arg))
	}
	return strings.Join(quoted, " ")
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	safe := true
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("_@%+=:,./-", r) {
			continue
		}
		safe = false
		break
	}
	if safe {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func (k Kubectl) runDemoPodAndMonitorImagePull(ctx context.Context, namespace, name string, args []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "kubectl", args...)
	var output safeBuffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			body := output.Bytes()
			if err != nil {
				return body, commandError("kubectl", args, body, err)
			}
			return body, nil
		case <-ticker.C:
			imageErr, err := k.demoPodImagePullFailure(ctx, namespace, name)
			if err != nil {
				continue
			}
			if imageErr == nil {
				continue
			}
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			<-done
			return output.Bytes(), imageErr
		case <-ctx.Done():
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			<-done
			return output.Bytes(), ctx.Err()
		}
	}
}

func (k Kubectl) demoPodImagePullFailure(ctx context.Context, namespace, name string) (error, error) {
	args := k.baseArgs("-n", namespace, "get", "pod", name, "-o", "json")
	out, err := runCommand(ctx, "", "kubectl", args...)
	if err != nil {
		if strings.Contains(err.Error(), "NotFound") || strings.Contains(err.Error(), "not found") {
			return nil, nil
		}
		return nil, err
	}
	var pod struct {
		Status struct {
			ContainerStatuses []struct {
				Image string `json:"image"`
				State struct {
					Waiting *struct {
						Reason  string `json:"reason"`
						Message string `json:"message"`
					} `json:"waiting"`
				} `json:"state"`
			} `json:"containerStatuses"`
		} `json:"status"`
	}
	if err := json.Unmarshal(out, &pod); err != nil {
		return nil, err
	}
	for _, status := range pod.Status.ContainerStatuses {
		if status.State.Waiting == nil {
			continue
		}
		reason := status.State.Waiting.Reason
		if reason != "ImagePullBackOff" && reason != "ErrImagePull" && reason != "InvalidImageName" {
			continue
		}
		return fmt.Errorf("image pull failed: image=%s reason=%s message=%s", status.Image, reason, status.State.Waiting.Message), nil
	}
	return nil, nil
}

type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.buf.Bytes()...)
}

func commandError(name string, args []string, output []byte, err error) error {
	message := strings.TrimSpace(string(output))
	if message == "" {
		message = err.Error()
	}
	return fmt.Errorf("%s %s failed: %s", name, strings.Join(args, " "), message)
}

func runCommand(ctx context.Context, stdin, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return nil, fmt.Errorf("%s %s failed: %s", name, strings.Join(args, " "), message)
	}
	return stdout.Bytes(), nil
}
