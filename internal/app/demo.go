package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const demoPodImage = "public.ecr.aws/aws-cli/aws-cli:latest"

type demoDefaults struct {
	Namespace      string
	ServiceAccount string
	RoleName       string
	PolicyName     string
	PodName        string
}

func (r *Runner) runDemoCreatePolicy(ctx context.Context, input demoCreatePolicyCommand, stdout io.Writer, progress Progress) error {
	progress.Start("Load state")
	state, stateDir, err := r.loadState(input.Name)
	if err != nil {
		progress.Fail(err.Error())
		return err
	}
	progress.Success(filepath.Join(stateDir, "state.json"))

	progress.Start("Resolve AWS account")
	effectiveProfile := chooseProfile(input.Profile, state.Profile)
	awsClient, err := r.AWSFactory.New(ctx, AWSOptions{Region: state.Region, Profile: effectiveProfile})
	if err != nil {
		progress.Fail(err.Error())
		return err
	}
	accountID, err := awsClient.AccountID(ctx)
	if err != nil {
		progress.Fail(err.Error())
		return err
	}
	if accountID != state.AccountID {
		err := fmt.Errorf("AWS account mismatch: got %s, want %s", accountID, state.AccountID)
		progress.Fail(err.Error())
		return err
	}
	progress.Success(accountID)

	defaults := newDemoDefaults(input.Name)
	document, err := demoPolicyDocument(accountID, defaults.RoleName)
	if err != nil {
		progress.Fail(err.Error())
		return err
	}
	request := DemoPolicyRequest{
		PolicyName: defaults.PolicyName,
		RoleName:   defaults.RoleName,
		Document:   document,
		Tags:       demoPolicyTags(state),
	}

	progress.Start("Ensure demo policy")
	details, err := awsClient.EnsureDemoPolicy(ctx, state, request)
	if err != nil {
		progress.Fail(err.Error())
		return err
	}
	progress.Success(details.PolicyARN)

	printDemoPolicyDetails(stdout, input.Name, defaults, details)
	return nil
}

func (r *Runner) runDemoRun(ctx context.Context, input demoRunCommand, stdout io.Writer, progress Progress) error {
	progress.Start("Load state")
	state, stateDir, err := r.loadState(input.Name)
	if err != nil {
		progress.Fail(err.Error())
		return err
	}
	progress.Success(filepath.Join(stateDir, "state.json"))

	defaults := newDemoDefaults(input.Name)
	namespace := chooseDefault(input.Namespace, defaults.Namespace)
	serviceAccount := chooseDefault(input.ServiceAccount, defaults.ServiceAccount)
	kube := r.KubectlFactory(input.ContextName)

	progress.Start("Check ServiceAccount")
	exists, err := kube.ServiceAccountExists(ctx, namespace, serviceAccount)
	if err != nil {
		progress.Fail(err.Error())
		return err
	}
	if !exists {
		err := fmt.Errorf("service account %s/%s does not exist", namespace, serviceAccount)
		progress.Fail(err.Error())
		return err
	}
	roleARN, err := kube.ServiceAccountRoleARN(ctx, namespace, serviceAccount)
	if err != nil {
		progress.Fail(err.Error())
		return err
	}
	roleName, err := roleNameFromARN(roleARN)
	if err != nil {
		progress.Fail(err.Error())
		return err
	}
	progress.Success(roleARN)

	command, err := demoPodRunCommand(input.ContextName, namespace, defaults.PodName, serviceAccount, state.Region, roleName)
	if err != nil {
		return err
	}

	podCreated := false
	cleanupAfterError := func(original error) error {
		if podCreated {
			cleanupDemoPod(ctx, kube, input.ContextName, namespace, defaults.PodName, progress)
		}
		return original
	}

	progress.Start("Run AWS CLI pod")
	podExists, err := kube.PodExists(ctx, namespace, defaults.PodName)
	if err != nil {
		progress.Fail(err.Error())
		return err
	}
	if podExists {
		err := fmt.Errorf("pod %s/%s already exists", namespace, defaults.PodName)
		progress.Fail(err.Error())
		return err
	}
	fmt.Fprintf(stdout, "\nCommand:\n%s\n", command)
	podCreated = true
	outputRaw, err := kube.RunDemoPod(ctx, namespace, defaults.PodName, serviceAccount, state.Region, roleName)
	output := string(outputRaw)
	if strings.TrimSpace(output) != "" {
		printDemoOutput(stdout, output)
	}
	if err != nil {
		progress.Fail(err.Error())
		return cleanupAfterError(err)
	}
	progress.Success(namespace + "/" + defaults.PodName)

	progress.Start("Check injected environment")
	if err := demoOutputHasEnvironmentChecks(output); err != nil {
		progress.Fail(err.Error())
		return cleanupAfterError(err)
	}
	progress.Success("environment found")

	progress.Start("Check AWS identity")
	assumedRoleARN, err := assumedRoleARNFromDemoLogs(output)
	if err != nil {
		progress.Fail(err.Error())
		return cleanupAfterError(err)
	}
	progress.Success(assumedRoleARN)

	progress.Start("Check IAM Role")
	checkedRoleARN, err := roleARNFromDemoLogs(output)
	if err != nil {
		progress.Fail(err.Error())
		return cleanupAfterError(err)
	}
	progress.Success(checkedRoleARN)

	cleanupDemoPod(ctx, kube, input.ContextName, namespace, defaults.PodName, progress)

	fmt.Fprintln(stdout, "\nDemo:")
	fmt.Fprintf(stdout, "  pod: %s/%s\n", namespace, defaults.PodName)
	fmt.Fprintf(stdout, "  assumed role: %s\n", assumedRoleARN)
	fmt.Fprintf(stdout, "  role: %s\n", checkedRoleARN)
	return nil
}

func (r *Runner) runDemoDeletePolicy(ctx context.Context, input demoDeletePolicyCommand, stdout io.Writer, progress Progress) error {
	progress.Start("Load state")
	state, stateDir, err := r.loadState(input.Name)
	if err != nil {
		progress.Fail(err.Error())
		return err
	}
	progress.Success(filepath.Join(stateDir, "state.json"))

	progress.Start("Resolve AWS account")
	effectiveProfile := chooseProfile(input.Profile, state.Profile)
	awsClient, err := r.AWSFactory.New(ctx, AWSOptions{Region: state.Region, Profile: effectiveProfile})
	if err != nil {
		progress.Fail(err.Error())
		return err
	}
	accountID, err := awsClient.AccountID(ctx)
	if err != nil {
		progress.Fail(err.Error())
		return err
	}
	if accountID != state.AccountID {
		err := fmt.Errorf("AWS account mismatch: got %s, want %s", accountID, state.AccountID)
		progress.Fail(err.Error())
		return err
	}
	progress.Success(accountID)

	defaults := newDemoDefaults(input.Name)
	request := DemoPolicyRequest{
		PolicyName: defaults.PolicyName,
		RoleName:   defaults.RoleName,
		Tags:       demoPolicyTags(state),
	}

	progress.Start("Delete demo policy")
	deleted, err := awsClient.DeleteDemoPolicy(ctx, state, request)
	if err != nil {
		progress.Fail(err.Error())
		return err
	}
	if deleted {
		progress.Success(customerManagedPolicyARN(state, defaults.PolicyName))
		fmt.Fprintf(stdout, "\nDemo policy:\n  status: deleted\n  name: %s\n", defaults.PolicyName)
		return nil
	}
	progress.Success("not found")
	fmt.Fprintf(stdout, "\nDemo policy:\n  status: not found\n  name: %s\n", defaults.PolicyName)
	return nil
}

func newDemoDefaults(name string) demoDefaults {
	safe := safeName(name)
	return demoDefaults{
		Namespace:      "default",
		ServiceAccount: "local-irsa-demo",
		RoleName:       "local-irsa-" + safe + "-demo",
		PolicyName:     "local-irsa-" + safe + "-demo",
		PodName:        "local-irsa-demo-" + safe,
	}
}

func demoPolicyDocument(accountID, roleName string) ([]byte, error) {
	return marshalPretty(map[string]any{
		"Version": "2012-10-17",
		"Statement": []map[string]any{
			{
				"Effect":   "Allow",
				"Action":   "sts:GetCallerIdentity",
				"Resource": "*",
			},
			{
				"Effect":   "Allow",
				"Action":   "iam:GetRole",
				"Resource": fmt.Sprintf("arn:aws:iam::%s:role/%s", accountID, roleName),
			},
		},
	})
}

func demoPolicyTags(state State) map[string]string {
	return mergeTags(ownershipTags(state), map[string]string{
		demoPurposeTagKey: demoPurposeTagValue,
	})
}

func printDemoPolicyDetails(stdout io.Writer, name string, defaults demoDefaults, details DemoPolicyDetails) {
	fmt.Fprintln(stdout, "\nDemo policy:")
	fmt.Fprintf(stdout, "  status: %s\n", details.Status)
	fmt.Fprintf(stdout, "  name: %s\n", details.PolicyName)
	fmt.Fprintf(stdout, "  arn: %s\n", details.PolicyARN)
	fmt.Fprintf(stdout, "  accountID: %s\n", details.AccountID)
	fmt.Fprintf(stdout, "  role name: %s\n", details.RoleName)
	fmt.Fprintf(stdout, "  default version: %s\n", details.DefaultVersionID)
	fmt.Fprintln(stdout, "  policy document:")
	writeIndented(stdout, string(details.Document), "    ")
	fmt.Fprintln(stdout, "  tags:")
	for _, key := range sortedKeys(details.Tags) {
		fmt.Fprintf(stdout, "    %s: %s\n", key, details.Tags[key])
	}
	fmt.Fprintln(stdout, "\nNext:")
	fmt.Fprintf(stdout, "  local-irsa bind --name %s --namespace %s --service-account %s --role-name %s --policy-arn %s --create-service-account\n", name, defaults.Namespace, defaults.ServiceAccount, defaults.RoleName, details.PolicyARN)
}

func printDemoOutput(stdout io.Writer, output string) {
	fmt.Fprintln(stdout, "\nOutput:")
	writeIndented(stdout, strings.TrimRight(output, "\n"), "  ")
}

func cleanupDemoPod(ctx context.Context, kube KubeClient, contextName, namespace, podName string, progress Progress) {
	progress.Start("Clean up demo pod")
	exists, err := kube.PodExists(ctx, namespace, podName)
	if err != nil {
		progress.Warn("failed to check demo pod cleanup: " + err.Error())
		progress.Info("Delete demo pod", demoPodDeleteCommand(contextName, namespace, podName))
		return
	}
	if !exists {
		progress.Success("removed by kubectl run --rm=true")
		return
	}
	progress.Warn("demo pod remained after kubectl run --rm=true")
	if err := kube.DeletePod(ctx, namespace, podName); err != nil {
		progress.Warn("failed to delete demo pod: " + err.Error())
		progress.Info("Delete demo pod", demoPodDeleteCommand(contextName, namespace, podName))
		return
	}
	progress.Success(namespace + "/" + podName)
}

func roleNameFromARN(roleARN string) (string, error) {
	parts := strings.SplitN(roleARN, ":", 6)
	if len(parts) != 6 || parts[0] != "arn" || parts[2] != "iam" || !strings.HasPrefix(parts[5], "role/") {
		return "", fmt.Errorf("invalid IAM Role ARN %q", roleARN)
	}
	rolePath := strings.TrimPrefix(parts[5], "role/")
	roleName := rolePath
	if index := strings.LastIndex(rolePath, "/"); index >= 0 {
		roleName = rolePath[index+1:]
	}
	if roleName == "" {
		return "", fmt.Errorf("invalid IAM Role ARN %q", roleARN)
	}
	return roleName, nil
}

func demoOutputHasEnvironmentChecks(output string) error {
	if strings.Contains(output, "test -n ") && strings.Contains(output, "test -f ") {
		return nil
	}
	return errors.New("demo output did not contain injected environment checks")
}

func assumedRoleARNFromDemoLogs(logs string) (string, error) {
	matches := regexp.MustCompile(`"Arn"\s*:\s*"([^"]+)"`).FindStringSubmatch(logs)
	if len(matches) != 2 || strings.TrimSpace(matches[1]) == "" {
		return "", errors.New("aws sts get-caller-identity output did not contain an ARN")
	}
	return matches[1], nil
}

func roleARNFromDemoLogs(logs string) (string, error) {
	for _, line := range strings.Split(logs, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "arn:") && strings.Contains(line, ":role/") {
			return line, nil
		}
	}
	return "", errors.New("aws iam get-role output did not contain a Role ARN")
}

func customerManagedPolicyARN(state State, policyName string) string {
	return fmt.Sprintf("arn:aws:iam::%s:policy/%s", state.AccountID, policyName)
}

func chooseDefault(value, defaultValue string) string {
	if value != "" {
		return value
	}
	return defaultValue
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func writeIndented(writer io.Writer, text, prefix string) {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	for _, line := range lines {
		fmt.Fprintln(writer, prefix+line)
	}
}
