package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/alecthomas/kong"
)

const (
	managedByTagKey        = "local-irsa.appthrust.io/managed-by"
	clusterTagKey          = "local-irsa.appthrust.io/cluster"
	saNamespaceTagKey      = "local-irsa.appthrust.io/service-account-namespace"
	saNameTagKey           = "local-irsa.appthrust.io/service-account-name"
	demoPurposeTagKey      = "local-irsa.appthrust.io/purpose"
	managedByTagValue      = "local-irsa"
	demoPurposeTagValue    = "demo"
	defaultTokenAudience   = "sts.amazonaws.com"
	defaultTokenExpiration = "86400"
)

type Runner struct {
	AWSFactory        AWSFactory
	KubectlFactory    func(contextName string) KubeClient
	IssuerIDGenerator func() (string, error)
	Env               func(string) string
	HomeDir           func() (string, error)
}

func NewRunner() *Runner {
	return &Runner{
		AWSFactory: ActualAWSFactory{},
		KubectlFactory: func(contextName string) KubeClient {
			return Kubectl{ContextName: contextName}
		},
		IssuerIDGenerator: generateIssuerID,
		Env:               os.Getenv,
		HomeDir:           os.UserHomeDir,
	}
}

type localIRSACommand struct {
	Quiet   bool `help:"Suppress successful progress lines and show only final results."`
	Verbose bool `help:"Show main AWS, S3, IAM, and kubectl operation targets."`

	Init    initCommand    `cmd:"" help:"Create local-irsa state and print a kind OIDC snippet."`
	Install installCommand `cmd:"" help:"Publish the issuer and install the pod identity webhook."`
	Bind    bindCommand    `cmd:"" help:"Create or update an IAM role binding for a ServiceAccount."`
	Unbind  unbindCommand  `cmd:"" help:"Remove one IAM role binding for a ServiceAccount."`
	Doctor  doctorCommand  `cmd:"" help:"Check the issuer, AWS resources, and an optional ServiceAccount."`
	Down    downCommand    `cmd:"" help:"Remove local-irsa managed resources."`
	Demo    demoCommand    `cmd:"" help:"Run demo helpers for an end-to-end local-irsa check."`
}

func (c *localIRSACommand) Validate() error {
	if c.Quiet && c.Verbose {
		return errors.New("--quiet and --verbose cannot be used together")
	}
	return nil
}

func (localIRSACommand) Help() string {
	return `Use init to choose cluster settings, install to publish the issuer, bind to connect a ServiceAccount to an IAM Role, unbind to remove one binding, doctor to check the setup, down to remove local-irsa managed resources, and demo to run a small end-to-end check.`
}

type initCommand struct {
	Name    string `required:"" placeholder:"NAME" help:"Cluster name used for local-irsa state."`
	Region  string `placeholder:"REGION" help:"AWS region. If empty, the AWS SDK resolves the region."`
	Bucket  string `placeholder:"BUCKET" help:"S3 bucket name for issuer documents. If empty, local-irsa builds one."`
	Profile string `placeholder:"PROFILE" help:"AWS profile name to use for this command."`
}

func (c initCommand) Help() string {
	return `Create local-irsa state for one cluster and print a kind config snippet. It does not create AWS or Kubernetes resources.

Example:
  local-irsa init --name dev --region ap-northeast-1`
}

type installCommand struct {
	Name        string `required:"" placeholder:"NAME" help:"Cluster name used for local-irsa state."`
	ContextName string `name:"context" placeholder:"CONTEXT" help:"Kubernetes context to use. If empty, kubectl uses the current context."`
	Profile     string `placeholder:"PROFILE" help:"AWS profile name to use for this command."`
	SkipWebhook bool   `help:"Skip cert-manager checks and webhook installation."`
}

func (c installCommand) Help() string {
	return `Read saved state and the current Kubernetes cluster, publish OIDC issuer documents to S3, and create or check the IAM OIDC Provider. By default, it also applies amazon-eks-pod-identity-webhook.

Example:
  local-irsa install --name dev --context kind-dev`
}

type bindCommand struct {
	Name                 string   `required:"" placeholder:"NAME" help:"Cluster name used for local-irsa state."`
	Namespace            string   `required:"" placeholder:"NS" help:"Kubernetes namespace that contains the ServiceAccount."`
	ServiceAccount       string   `required:"" placeholder:"SA" help:"Kubernetes ServiceAccount to bind to the IAM Role."`
	RoleName             string   `required:"" placeholder:"ROLE" help:"IAM Role name to create or update."`
	PolicyARNs           []string `name:"policy-arn" required:"" placeholder:"ARN" help:"Existing managed policy ARN to attach. Repeat for more than one policy."`
	ContextName          string   `name:"context" placeholder:"CONTEXT" help:"Kubernetes context to use. If empty, kubectl uses the current context."`
	Profile              string   `placeholder:"PROFILE" help:"AWS profile name to use for this command."`
	CreateServiceAccount bool     `help:"Create the ServiceAccount when it does not exist."`
}

func (c bindCommand) Help() string {
	return `Create or update the IAM Role for one Kubernetes ServiceAccount, attach existing managed policy ARNs, and write the IRSA annotation.

Example:
  local-irsa bind --name dev --namespace default --service-account app --role-name app-dev --policy-arn arn:aws:iam::aws:policy/AmazonS3ReadOnlyAccess`
}

type unbindCommand struct {
	Name           string `required:"" placeholder:"NAME" help:"Cluster name used for local-irsa state."`
	Namespace      string `required:"" placeholder:"NS" help:"Kubernetes namespace that contains the ServiceAccount."`
	ServiceAccount string `required:"" placeholder:"SA" help:"Kubernetes ServiceAccount to unbind from its IAM Role."`
	ContextName    string `name:"context" placeholder:"CONTEXT" help:"Kubernetes context to use. If empty, kubectl uses the current context."`
	Profile        string `placeholder:"PROFILE" help:"AWS profile name to use for this command."`
}

func (c unbindCommand) Help() string {
	return `Remove one ServiceAccount binding by deleting local-irsa annotations, detaching managed policies, deleting the IAM Role, and updating state.

Example:
  local-irsa unbind --name dev --namespace default --service-account app`
}

type doctorCommand struct {
	Name           string `required:"" placeholder:"NAME" help:"Cluster name used for local-irsa state."`
	Namespace      string `placeholder:"NS" help:"Kubernetes namespace to check with --service-account."`
	ServiceAccount string `placeholder:"SA" help:"Kubernetes ServiceAccount to check with --namespace."`
	ContextName    string `name:"context" placeholder:"CONTEXT" help:"Kubernetes context to use. If empty, kubectl uses the current context."`
	Profile        string `placeholder:"PROFILE" help:"AWS profile name to use for this command."`
}

func (c doctorCommand) Help() string {
	return `Check that state, cluster OIDC settings, S3 issuer documents, and the IAM OIDC Provider match. When a ServiceAccount is specified, also check token exchange.

Example:
  local-irsa doctor --name dev --namespace default --service-account app`
}

type downCommand struct {
	Name         string `required:"" placeholder:"NAME" help:"Cluster name used for local-irsa state."`
	ContextName  string `name:"context" placeholder:"CONTEXT" help:"Kubernetes context to use. If empty, kubectl uses the current context."`
	Profile      string `placeholder:"PROFILE" help:"AWS profile name to use for this command."`
	DeleteBucket bool   `help:"Delete the issuer S3 bucket after removing issuer objects."`
	Yes          bool   `help:"Skip the confirmation prompt."`
}

func (c downCommand) Help() string {
	return `Remove local-irsa managed ServiceAccount annotations, IAM Roles, IAM OIDC Provider, and issuer objects. It keeps the S3 bucket unless --delete-bucket is set.

Example:
  local-irsa down --name dev --yes`
}

type demoCommand struct {
	CreatePolicy demoCreatePolicyCommand `cmd:"" name:"create-policy" help:"Create or check the demo IAM policy."`
	Run          demoRunCommand          `cmd:"" help:"Run an AWS CLI pod with a bound ServiceAccount."`
	DeletePolicy demoDeletePolicyCommand `cmd:"" name:"delete-policy" help:"Delete the demo IAM policy."`
}

func (c demoCommand) Help() string {
	return `Create a demo policy, run a bound AWS CLI pod, or delete the demo policy.

Example:
  local-irsa demo create-policy --name dev`
}

type demoCreatePolicyCommand struct {
	Name    string `required:"" placeholder:"NAME" help:"Cluster name used for local-irsa state."`
	Profile string `placeholder:"PROFILE" help:"AWS profile name to use for this command."`
}

func (c demoCreatePolicyCommand) Help() string {
	return `Create or check the small customer managed policy used by the demo flow. It does not run bind.

Example:
  local-irsa demo create-policy --name dev`
}

type demoRunCommand struct {
	Name           string `required:"" placeholder:"NAME" help:"Cluster name used for local-irsa state."`
	Namespace      string `placeholder:"NS" help:"Kubernetes namespace that contains the ServiceAccount. Defaults to default."`
	ServiceAccount string `placeholder:"SA" help:"Kubernetes ServiceAccount to test. Defaults to local-irsa-demo."`
	ContextName    string `name:"context" placeholder:"CONTEXT" help:"Kubernetes context to use. If empty, kubectl uses the current context."`
}

func (c demoRunCommand) Help() string {
	return `Run an AWS CLI pod with a bound ServiceAccount and verify web identity credentials.

Example:
  local-irsa demo run --name dev`
}

type demoDeletePolicyCommand struct {
	Name    string `required:"" placeholder:"NAME" help:"Cluster name used for local-irsa state."`
	Profile string `placeholder:"PROFILE" help:"AWS profile name to use for this command."`
}

func (c demoDeletePolicyCommand) Help() string {
	return `Delete the demo customer managed policy when local-irsa roles are already removed.

Example:
  local-irsa demo delete-policy --name dev`
}

type commandRuntime struct {
	Context  context.Context
	Runner   *Runner
	Stdin    io.Reader
	Stdout   io.Writer
	Stderr   io.Writer
	Progress Progress
}

func (c *initCommand) Run(runtime *commandRuntime) error {
	return runtime.Runner.runInit(runtime.Context, *c, runtime.Stdout, runtime.Progress)
}

func (c *installCommand) Run(runtime *commandRuntime) error {
	return runtime.Runner.runInstall(runtime.Context, *c, runtime.Stdout, runtime.Progress)
}

func (c *bindCommand) Run(runtime *commandRuntime) error {
	return runtime.Runner.runBind(runtime.Context, *c, runtime.Stdout, runtime.Progress)
}

func (c *unbindCommand) Run(runtime *commandRuntime) error {
	return runtime.Runner.runUnbind(runtime.Context, *c, runtime.Stdout, runtime.Progress)
}

func (c *doctorCommand) Run(runtime *commandRuntime) error {
	return runtime.Runner.runDoctor(runtime.Context, *c, runtime.Stdout, runtime.Progress)
}

func (c *downCommand) Run(runtime *commandRuntime) error {
	return runtime.Runner.runDown(runtime.Context, *c, runtime.Stdin, runtime.Stdout, runtime.Progress)
}

func (c *demoCreatePolicyCommand) Run(runtime *commandRuntime) error {
	return runtime.Runner.runDemoCreatePolicy(runtime.Context, *c, runtime.Stdout, runtime.Progress)
}

func (c *demoRunCommand) Run(runtime *commandRuntime) error {
	return runtime.Runner.runDemoRun(runtime.Context, *c, runtime.Stdout, runtime.Progress)
}

func (c *demoDeletePolicyCommand) Run(runtime *commandRuntime) error {
	return runtime.Runner.runDemoDeletePolicy(runtime.Context, *c, runtime.Stdout, runtime.Progress)
}

func (r *Runner) Execute(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if r.AWSFactory == nil {
		r.AWSFactory = ActualAWSFactory{}
	}
	if r.KubectlFactory == nil {
		r.KubectlFactory = func(contextName string) KubeClient { return Kubectl{ContextName: contextName} }
	}
	if r.IssuerIDGenerator == nil {
		r.IssuerIDGenerator = generateIssuerID
	}
	if r.Env == nil {
		r.Env = os.Getenv
	}
	if r.HomeDir == nil {
		r.HomeDir = os.UserHomeDir
	}

	cli := localIRSACommand{}
	helpRequested := false
	parser, err := kong.New(&cli,
		kong.Name("local-irsa"),
		kong.Description("Manage IAM Roles for Service Accounts for a local Kubernetes cluster."),
		kong.Writers(stdout, stderr),
		kong.Exit(func(code int) {
			if code == 0 {
				helpRequested = true
			}
		}),
		kong.UsageOnError(),
	)
	if err != nil {
		fmt.Fprintf(stderr, "local-irsa: %v\n", err)
		return err
	}

	parsed, err := parser.Parse(args)
	if helpRequested {
		return nil
	}
	if err != nil {
		parser.Stdout = stderr
		parser.Stderr = stderr
		parser.FatalIfErrorf(err)
		return err
	}
	err = parsed.Run(&commandRuntime{
		Context:  ctx,
		Runner:   r,
		Stdin:    stdin,
		Stdout:   stdout,
		Stderr:   stderr,
		Progress: newProgress(stderr, cli.Quiet, cli.Verbose),
	})
	if err != nil {
		fmt.Fprintf(stderr, "local-irsa: %v\n", err)
	}
	return err
}

func (r *Runner) runInit(ctx context.Context, input initCommand, stdout io.Writer, progress Progress) error {
	progress.Start("Resolve AWS settings")
	awsClient, err := r.AWSFactory.New(ctx, AWSOptions{Region: input.Region, Profile: input.Profile})
	if err != nil {
		progress.Fail(err.Error())
		return err
	}
	resolvedRegion := awsClient.Region()
	if resolvedRegion == "" {
		err := errors.New("AWS region could not be resolved")
		progress.Fail(err.Error())
		return err
	}
	progress.Success("region " + resolvedRegion)

	progress.Start("Get AWS account ID")
	accountID, err := awsClient.AccountID(ctx)
	if err != nil {
		progress.Fail(err.Error())
		return err
	}
	if accountID == "" {
		err := errors.New("AWS account ID is empty")
		progress.Fail(err.Error())
		return err
	}
	progress.Success(accountID)

	progress.Start("Decide issuer URL")
	stateDir, err := r.stateDir(input.Name)
	if err != nil {
		progress.Fail(err.Error())
		return err
	}
	statePath := filepath.Join(stateDir, "state.json")
	existing, existingErr := loadStateFile(statePath)

	resolvedBucket := input.Bucket
	issuerID := ""
	if resolvedBucket == "" {
		if existingErr == nil && existing.IssuerID != "" {
			issuerID = existing.IssuerID
		} else {
			issuerID, err = r.IssuerIDGenerator()
			if err != nil {
				progress.Fail(err.Error())
				return err
			}
		}
		if err := validateIssuerID(issuerID); err != nil {
			progress.Fail(err.Error())
			return err
		}
		resolvedBucket = defaultBucketName(accountID, resolvedRegion, input.Name, issuerID)
	}
	issuerURL := issuerURLForBucket(resolvedBucket, resolvedRegion)
	progress.Detail("S3 bucket", resolvedBucket)
	progress.Success(issuerURL)

	next := State{
		Name:      input.Name,
		Region:    resolvedRegion,
		IssuerID:  issuerID,
		Bucket:    resolvedBucket,
		IssuerURL: issuerURL,
		AccountID: accountID,
	}
	if input.Profile != "" {
		next.Profile = input.Profile
	}

	if existingErr == nil {
		if existing.AccountID != accountID || existing.Region != resolvedRegion || existing.IssuerID != issuerID || existing.Bucket != resolvedBucket || existing.IssuerURL != issuerURL {
			err := errors.New("existing state has different accountID, region, issuerID, bucket, or issuerURL")
			progress.Fail(err.Error())
			return err
		}
		next.Bindings = existing.Bindings
		if input.Profile == "" {
			next.Profile = existing.Profile
		}
	} else if !errors.Is(existingErr, os.ErrNotExist) {
		progress.Fail(existingErr.Error())
		return existingErr
	}

	progress.Start("Write state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		progress.Fail(err.Error())
		return err
	}
	if err := saveStateFile(statePath, next); err != nil {
		progress.Fail(err.Error())
		return err
	}
	progress.Success(statePath)

	progress.Start("Write kind snippet")
	snippetPath := filepath.Join(stateDir, "kind-irsa-snippet.yaml")
	snippet := kindSnippet(issuerURL)
	if err := os.WriteFile(snippetPath, []byte(snippet), 0o644); err != nil {
		progress.Fail(err.Error())
		return err
	}
	progress.Success(snippetPath)

	fmt.Fprintf(stdout, "\nState:\n  path: %s\n", statePath)
	fmt.Fprintf(stdout, "Kind snippet:\n  path: %s\n", snippetPath)
	fmt.Fprintf(stdout, "Issuer:\n  url: %s\n\n", issuerURL)
	fmt.Fprintln(stdout, "Next:")
	fmt.Fprintln(stdout, "  1. Merge the kind snippet into kind.yaml.")
	fmt.Fprintln(stdout, "  2. Run kind create cluster --config kind.yaml.")
	fmt.Fprintf(stdout, "  3. Run local-irsa install --name %s.\n", input.Name)
	return nil
}

func (r *Runner) runInstall(ctx context.Context, input installCommand, stdout io.Writer, progress Progress) error {
	progress.Start("Load state")
	state, stateDir, err := r.loadState(input.Name)
	if err != nil {
		progress.Fail(err.Error())
		return err
	}
	statePath := filepath.Join(stateDir, "state.json")
	progress.Success(statePath)

	effectiveProfile := chooseProfile(input.Profile, state.Profile)
	awsClient, err := r.AWSFactory.New(ctx, AWSOptions{Region: state.Region, Profile: effectiveProfile})
	if err != nil {
		progress.Fail(err.Error())
		return err
	}
	kube := r.KubectlFactory(input.ContextName)

	progress.Start("Check cluster OIDC")
	if _, err := readClusterDiscovery(ctx, kube, state); err != nil {
		progress.Fail(err.Error())
		return err
	}
	progress.Success("issuer matches state")

	progress.Start("Read cluster JWKS")
	jwksRaw, keyCount, err := readClusterJWKS(ctx, kube)
	if err != nil {
		progress.Fail(err.Error())
		return err
	}
	progress.Success(fmt.Sprintf("%d key", keyCount))

	discovery := buildDiscoveryDocument(state.IssuerURL)
	discoveryBytes, err := marshalPretty(discovery)
	if err != nil {
		progress.Fail(err.Error())
		return err
	}
	jwksBytes, err := normalizeJSON(jwksRaw)
	if err != nil {
		progress.Fail(err.Error())
		return err
	}

	if !input.SkipWebhook {
		progress.Start("Check webhook prerequisites")
		if err := kube.CheckCertManager(ctx); err != nil {
			progress.Fail(err.Error())
			return err
		}
		progress.Success("cert-manager Certificate CRD found")
	}

	progress.Start("Prepare S3 issuer")
	progress.Detail("S3 bucket", state.Bucket)
	progress.Detail("S3 region", state.Region)
	progress.Success("s3://" + state.Bucket)

	progress.Start("Publish issuer documents")
	if err := awsClient.EnsureIssuer(ctx, state, discoveryBytes, jwksBytes); err != nil {
		progress.Fail(err.Error())
		return err
	}
	progress.Success("/.well-known/openid-configuration, /keys.json")

	progress.Start("Ensure IAM OIDC Provider")
	providerARN, err := awsClient.EnsureOIDCProvider(ctx, state)
	if err != nil {
		progress.Fail(err.Error())
		return err
	}
	progress.Success(providerARN)

	if err := os.WriteFile(filepath.Join(stateDir, "openid-configuration.json"), discoveryBytes, 0o644); err != nil {
		progress.Fail(err.Error())
		return err
	}
	if err := os.WriteFile(filepath.Join(stateDir, "keys.json"), jwksBytes, 0o644); err != nil {
		progress.Fail(err.Error())
		return err
	}
	if !input.SkipWebhook {
		progress.Start("Apply webhook")
		if err := kube.Apply(ctx, webhookManifest()); err != nil {
			progress.Fail(err.Error())
			return err
		}
		progress.Success("applied")

		progress.Start("Check webhook readiness")
		if err := kube.WaitDeploymentAvailable(ctx, "local-irsa-system", "pod-identity-webhook", "120s"); err != nil {
			progress.Fail(err.Error())
			return err
		}
		if err := kube.CheckServiceAccountPermissions(ctx, "local-irsa-system", "pod-identity-webhook", []string{"get", "list", "watch"}, "serviceaccounts"); err != nil {
			progress.Fail(err.Error())
			return err
		}
		progress.Success("deployment/pod-identity-webhook")

		progress.Start("Check webhook mutation")
		if err := kube.CheckWebhookMutation(ctx, state.AccountID, state.Region); err != nil {
			progress.Fail(err.Error())
			return err
		}
		progress.Success("server-side dry-run")
	}

	fmt.Fprintf(stdout, "\nIssuer:\n  discovery: %s/.well-known/openid-configuration\n", state.IssuerURL)
	fmt.Fprintf(stdout, "  jwks: %s/keys.json\n", state.IssuerURL)
	fmt.Fprintf(stdout, "IAM:\n  oidc provider: %s\n", providerARN)
	if input.SkipWebhook {
		fmt.Fprintln(stdout, "Webhook:\n  status: skipped")
	} else {
		fmt.Fprintln(stdout, "Webhook:\n  status: applied")
	}
	return nil
}

func (r *Runner) runBind(ctx context.Context, input bindCommand, stdout io.Writer, progress Progress) error {
	if len(input.PolicyARNs) == 0 {
		return errors.New("at least one --policy-arn is required")
	}

	progress.Start("Load state")
	state, stateDir, err := r.loadState(input.Name)
	if err != nil {
		progress.Fail(err.Error())
		return err
	}
	progress.Success(filepath.Join(stateDir, "state.json"))

	effectiveProfile := chooseProfile(input.Profile, state.Profile)
	awsClient, err := r.AWSFactory.New(ctx, AWSOptions{Region: state.Region, Profile: effectiveProfile})
	if err != nil {
		progress.Fail(err.Error())
		return err
	}
	kube := r.KubectlFactory(input.ContextName)
	binding := Binding{
		Namespace:      input.Namespace,
		ServiceAccount: input.ServiceAccount,
		RoleName:       input.RoleName,
		PolicyARNs:     append([]string(nil), input.PolicyARNs...),
	}

	progress.Start("Ensure IAM Role")
	progress.Detail("IAM Role", input.RoleName)
	roleARN, err := awsClient.EnsureRole(ctx, state, binding)
	if err != nil {
		progress.Fail(err.Error())
		return err
	}
	binding.RoleARN = roleARN
	progress.Success(roleARN)

	progress.Start("Attach managed policies")
	progress.Success(fmt.Sprintf("%d policy", len(input.PolicyARNs)))

	progress.Start("Check ServiceAccount")
	exists, err := kube.ServiceAccountExists(ctx, input.Namespace, input.ServiceAccount)
	if err != nil {
		progress.Fail(err.Error())
		return err
	}
	if !exists {
		if !input.CreateServiceAccount {
			err := fmt.Errorf("service account %s/%s does not exist", input.Namespace, input.ServiceAccount)
			progress.Fail(err.Error())
			return err
		}
		if err := kube.CreateServiceAccount(ctx, input.Namespace, input.ServiceAccount); err != nil {
			progress.Fail(err.Error())
			return err
		}
		progress.Success("created " + input.Namespace + "/" + input.ServiceAccount)
	} else {
		progress.Success("found " + input.Namespace + "/" + input.ServiceAccount)
	}

	progress.Start("Annotate ServiceAccount")
	annotations := map[string]string{
		"eks.amazonaws.com/role-arn":               roleARN,
		"eks.amazonaws.com/audience":               defaultTokenAudience,
		"eks.amazonaws.com/sts-regional-endpoints": "true",
		"eks.amazonaws.com/token-expiration":       defaultTokenExpiration,
	}
	if err := kube.AnnotateServiceAccount(ctx, input.Namespace, input.ServiceAccount, annotations); err != nil {
		progress.Fail(err.Error())
		return err
	}
	progress.Success(input.Namespace + "/" + input.ServiceAccount)

	progress.Start("Save binding")
	state.Bindings = upsertBinding(state.Bindings, binding)
	if err := saveStateFile(filepath.Join(stateDir, "state.json"), state); err != nil {
		progress.Fail(err.Error())
		return err
	}
	progress.Success(filepath.Join(stateDir, "state.json"))

	fmt.Fprintf(stdout, "\nBinding:\n  serviceAccount: %s/%s\n  role: %s\n", input.Namespace, input.ServiceAccount, roleARN)
	return nil
}

func (r *Runner) runUnbind(ctx context.Context, input unbindCommand, stdout io.Writer, progress Progress) error {
	progress.Start("Load state")
	state, stateDir, err := r.loadState(input.Name)
	if err != nil {
		progress.Fail(err.Error())
		return err
	}
	statePath := filepath.Join(stateDir, "state.json")
	progress.Success(statePath)

	progress.Start("Find binding")
	binding, ok := findBinding(state.Bindings, input.Namespace, input.ServiceAccount)
	if !ok {
		err := fmt.Errorf("binding %s/%s does not exist in state", input.Namespace, input.ServiceAccount)
		progress.Fail(err.Error())
		return err
	}
	progress.Success(binding.RoleName)

	effectiveProfile := chooseProfile(input.Profile, state.Profile)
	awsClient, err := r.AWSFactory.New(ctx, AWSOptions{Region: state.Region, Profile: effectiveProfile})
	if err != nil {
		progress.Fail(err.Error())
		return err
	}
	kube := r.KubectlFactory(input.ContextName)

	progress.Start("Remove ServiceAccount annotations")
	removedAnnotations, err := removeServiceAccountAnnotations(ctx, kube, binding)
	if err != nil {
		progress.Fail(err.Error())
		return err
	}
	if removedAnnotations {
		progress.Success(binding.Namespace + "/" + binding.ServiceAccount)
	} else {
		progress.Success("skipped missing " + binding.Namespace + "/" + binding.ServiceAccount)
	}

	progress.Start("Detach managed policies")
	deleted, err := awsClient.CleanupRole(ctx, state, binding)
	if err != nil {
		progress.Fail(err.Error())
		return err
	}
	if !deleted {
		err := fmt.Errorf("IAM Role %s is not owned by local-irsa cluster %s", binding.RoleName, state.Name)
		progress.Fail(err.Error())
		return err
	}
	progress.Success(fmt.Sprintf("%d policy", len(binding.PolicyARNs)))

	progress.Start("Delete IAM Role")
	progress.Success(binding.RoleName)

	progress.Start("Save binding")
	state.Bindings = removeBinding(state.Bindings, binding)
	if err := saveStateFile(statePath, state); err != nil {
		progress.Fail(err.Error())
		return err
	}
	progress.Success(statePath)

	fmt.Fprintf(stdout, "\nUnbound:\n  serviceAccount: %s/%s\n  role: %s\n", binding.Namespace, binding.ServiceAccount, binding.RoleName)
	return nil
}

func (r *Runner) runDoctor(ctx context.Context, input doctorCommand, stdout io.Writer, progress Progress) error {
	if (input.Namespace == "") != (input.ServiceAccount == "") {
		return errors.New("--namespace and --service-account must be specified together")
	}

	progress.Start("Load state")
	state, _, err := r.loadState(input.Name)
	if err != nil {
		progress.Fail(err.Error())
		return err
	}
	stateDir, err := r.stateDir(input.Name)
	if err != nil {
		progress.Fail(err.Error())
		return err
	}
	statePath := filepath.Join(stateDir, "state.json")
	progress.Success(statePath)

	if !progressQuiet(progress) {
		if err := writeDoctorStateReport(stdout, statePath, state, shouldColorDoctorState(stdout)); err != nil {
			progress.Fail(err.Error())
			return err
		}
	}

	effectiveProfile := chooseProfile(input.Profile, state.Profile)
	awsClient, err := r.AWSFactory.New(ctx, AWSOptions{Region: state.Region, Profile: effectiveProfile})
	if err != nil {
		progress.Fail(err.Error())
		return err
	}
	kube := r.KubectlFactory(input.ContextName)

	progress.Start("Check cluster OIDC")
	if _, err := readClusterDiscovery(ctx, kube, state); err != nil {
		progress.Fail(err.Error())
		return err
	}
	progress.Success("issuer matches state")

	progress.Start("Read cluster JWKS")
	jwksRaw, keyCount, err := readClusterJWKS(ctx, kube)
	if err != nil {
		progress.Fail(err.Error())
		return err
	}
	progress.Success(fmt.Sprintf("%d key", keyCount))

	discoveryBytes, err := marshalPretty(buildDiscoveryDocument(state.IssuerURL))
	if err != nil {
		progress.Fail(err.Error())
		return err
	}
	jwksBytes, err := normalizeJSON(jwksRaw)
	if err != nil {
		progress.Fail(err.Error())
		return err
	}

	progress.Start("Check S3 issuer")
	if err := awsClient.CheckIssuerObjects(ctx, state, discoveryBytes, jwksBytes); err != nil {
		progress.Fail(err.Error())
		return err
	}
	progress.Success("matches cluster")

	progress.Start("Check IAM OIDC Provider")
	if err := awsClient.CheckOIDCProvider(ctx, state); err != nil {
		progress.Fail(err.Error())
		return err
	}
	progress.Success(oidcProviderARN(state))

	var assumedARN string
	if input.Namespace != "" {
		progress.Start("Check ServiceAccount binding")
		roleARN, err := kube.ServiceAccountRoleARN(ctx, input.Namespace, input.ServiceAccount)
		if err != nil {
			progress.Fail(err.Error())
			return err
		}
		progress.Success(roleARN)

		progress.Start("Test web identity")
		token, err := kube.CreateToken(ctx, input.Namespace, input.ServiceAccount, defaultTokenAudience, "15m")
		if err != nil {
			progress.Fail(err.Error())
			return err
		}
		assumedARN, err = awsClient.AssumeRoleWithWebIdentity(ctx, roleARN, token, 900)
		if err != nil {
			progress.Fail(err.Error())
			return err
		}
		progress.Success(assumedARN)
	}
	fmt.Fprintln(stdout, "\nDoctor:")
	fmt.Fprintln(stdout, "  oidc issuer: ok")
	if assumedARN != "" {
		fmt.Fprintf(stdout, "  assumed role: %s\n", assumedARN)
	}
	return nil
}

func (r *Runner) runDown(ctx context.Context, input downCommand, stdin io.Reader, stdout io.Writer, progress Progress) error {
	if !input.Yes {
		fmt.Fprintf(stdout, "Delete local-irsa resources for %s?\n  Type y to continue: ", input.Name)
		var answer string
		if _, err := fmt.Fscan(stdin, &answer); err != nil {
			return err
		}
		if answer != "y" {
			fmt.Fprintln(stdout, "Aborted")
			return nil
		}
	}

	progress.Start("Load state")
	state, stateDir, err := r.loadState(input.Name)
	if err != nil {
		progress.Fail(err.Error())
		return err
	}
	statePath := filepath.Join(stateDir, "state.json")
	progress.Success(statePath)

	effectiveProfile := chooseProfile(input.Profile, state.Profile)
	awsClient, err := r.AWSFactory.New(ctx, AWSOptions{Region: state.Region, Profile: effectiveProfile})
	if err != nil {
		progress.Fail(err.Error())
		return err
	}
	kube := r.KubectlFactory(input.ContextName)

	progress.Start("Remove ServiceAccount annotations")
	removedServiceAccounts := 0
	for _, binding := range state.Bindings {
		removed, err := removeServiceAccountAnnotations(ctx, kube, binding)
		if err != nil {
			progress.Fail(err.Error())
			return err
		}
		if removed {
			removedServiceAccounts++
		}
	}
	progress.Success(fmt.Sprintf("%d serviceAccount", removedServiceAccounts))

	progress.Start("Delete IAM roles")
	remainingBindings := make([]Binding, 0, len(state.Bindings))
	deletedBindings := 0
	for _, binding := range state.Bindings {
		deleted, err := awsClient.CleanupRole(ctx, state, binding)
		if err != nil {
			progress.Fail(err.Error())
			remainingBindings = append(remainingBindings, binding)
			state.Bindings = append(remainingBindings, bindingsAfter(state.Bindings, binding)...)
			if saveErr := saveStateFile(statePath, state); saveErr != nil {
				return fmt.Errorf("%w; additionally failed to save state: %v", err, saveErr)
			}
			return err
		}
		if !deleted {
			progress.Warn(fmt.Sprintf("skipped non-owned role %s", binding.RoleName))
			remainingBindings = append(remainingBindings, binding)
			continue
		}
		deletedBindings++
	}
	state.Bindings = remainingBindings
	if err := saveStateFile(statePath, state); err != nil {
		progress.Fail(err.Error())
		return err
	}
	progress.Success(fmt.Sprintf("%d role", deletedBindings))

	progress.Start("Delete IAM OIDC Provider")
	providerDeleted, err := awsClient.CleanupProvider(ctx, state)
	if err != nil {
		progress.Fail(err.Error())
		return err
	}
	if !providerDeleted {
		progress.Warn("skipped non-owned IAM OIDC Provider")
	}
	progress.Success(oidcProviderARN(state))

	progress.Start("Delete S3 issuer objects")
	issuerCleaned, err := awsClient.CleanupIssuer(ctx, state, false)
	if err != nil {
		progress.Fail(err.Error())
		return err
	}
	if !issuerCleaned {
		progress.Warn(fmt.Sprintf("skipped non-owned S3 bucket %s", state.Bucket))
	}
	progress.Success("s3://" + state.Bucket)

	bucketDeleted := false
	if input.DeleteBucket {
		progress.Start("Delete S3 bucket")
		if !providerDeleted {
			progress.Warn("skipped because IAM OIDC Provider remains")
			progress.Success("kept " + state.Bucket)
		} else if !issuerCleaned {
			progress.Warn("skipped because S3 issuer objects were not deleted")
			progress.Success("kept " + state.Bucket)
		} else {
			bucketDeleted, err = awsClient.CleanupIssuer(ctx, state, true)
			if err != nil {
				progress.Fail(err.Error())
				return err
			}
			if !bucketDeleted {
				progress.Warn(fmt.Sprintf("skipped non-owned S3 bucket %s", state.Bucket))
			}
			progress.Success(state.Bucket)
		}
	} else if issuerCleaned {
		progress.Info("S3 bucket kept", "s3://"+state.Bucket)
		progress.Info("Delete bucket", fmt.Sprintf("local-irsa down --name %s --delete-bucket", input.Name))
	}

	if input.DeleteBucket && len(state.Bindings) == 0 && providerDeleted && issuerCleaned && bucketDeleted {
		if err := os.RemoveAll(stateDir); err != nil {
			progress.Fail(err.Error())
			return err
		}
	}
	fmt.Fprintln(stdout, "\nRemoved local-irsa managed resources")
	return nil
}

type State struct {
	Name      string    `json:"name"`
	Region    string    `json:"region"`
	IssuerID  string    `json:"issuerID,omitempty"`
	Bucket    string    `json:"bucket"`
	IssuerURL string    `json:"issuerURL"`
	AccountID string    `json:"accountID"`
	Profile   string    `json:"profile,omitempty"`
	Bindings  []Binding `json:"bindings,omitempty"`
}

type Binding struct {
	Namespace      string   `json:"namespace"`
	ServiceAccount string   `json:"serviceAccount"`
	RoleName       string   `json:"roleName"`
	RoleARN        string   `json:"roleARN"`
	PolicyARNs     []string `json:"policyARNs"`
}

func (r *Runner) stateRoot() (string, error) {
	if root := r.Env("LOCAL_IRSA_STATE_ROOT"); root != "" {
		return root, nil
	}
	home, err := r.HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "local-irsa"), nil
}

func (r *Runner) stateDir(name string) (string, error) {
	root, err := r.stateRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "clusters", name), nil
}

func (r *Runner) loadState(name string) (State, string, error) {
	stateDir, err := r.stateDir(name)
	if err != nil {
		return State{}, "", err
	}
	state, err := loadStateFile(filepath.Join(stateDir, "state.json"))
	if err != nil {
		return State{}, "", err
	}
	return state, stateDir, nil
}

func loadStateFile(path string) (State, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return State{}, err
	}
	var state State
	if err := json.Unmarshal(body, &state); err != nil {
		return State{}, err
	}
	return state, nil
}

func saveStateFile(path string, state State) error {
	body, err := marshalPretty(state)
	if err != nil {
		return err
	}
	return os.WriteFile(path, body, 0o644)
}

func bindingsAfter(bindings []Binding, current Binding) []Binding {
	for i, binding := range bindings {
		if binding.Namespace == current.Namespace && binding.ServiceAccount == current.ServiceAccount && binding.RoleName == current.RoleName {
			return append([]Binding(nil), bindings[i+1:]...)
		}
	}
	return nil
}

func findBinding(bindings []Binding, namespace, serviceAccount string) (Binding, bool) {
	for _, binding := range bindings {
		if binding.Namespace == namespace && binding.ServiceAccount == serviceAccount {
			return binding, true
		}
	}
	return Binding{}, false
}

func removeBinding(bindings []Binding, target Binding) []Binding {
	out := bindings[:0]
	for _, binding := range bindings {
		if binding.Namespace == target.Namespace && binding.ServiceAccount == target.ServiceAccount {
			continue
		}
		out = append(out, binding)
	}
	return out
}

func removeServiceAccountAnnotations(ctx context.Context, kube KubeClient, binding Binding) (bool, error) {
	exists, err := kube.ServiceAccountExists(ctx, binding.Namespace, binding.ServiceAccount)
	if err != nil {
		return false, err
	}
	if !exists {
		return false, nil
	}
	if err := kube.RemoveServiceAccountAnnotations(ctx, binding.Namespace, binding.ServiceAccount, localIRSAServiceAccountAnnotations()); err != nil {
		return false, err
	}
	return true, nil
}

func localIRSAServiceAccountAnnotations() []string {
	return []string{
		"eks.amazonaws.com/role-arn",
		"eks.amazonaws.com/audience",
		"eks.amazonaws.com/sts-regional-endpoints",
		"eks.amazonaws.com/token-expiration",
	}
}

func writeDoctorStateReport(w io.Writer, statePath string, state State, color bool) error {
	body, err := marshalPretty(state)
	if err != nil {
		return err
	}
	if color {
		body = []byte(highlightJSON(string(body)))
	}
	fmt.Fprintf(w, "\nState:\n  path: %s\n  json:\n", statePath)
	for _, line := range strings.SplitAfter(string(body), "\n") {
		if line == "" {
			continue
		}
		fmt.Fprint(w, "    "+line)
	}
	return nil
}

func shouldColorDoctorState(w io.Writer) bool {
	return isTerminal(w) && os.Getenv("NO_COLOR") == ""
}

func progressQuiet(progress Progress) bool {
	if p, ok := progress.(*cliProgress); ok {
		return p.quiet
	}
	return false
}

func highlightJSON(input string) string {
	var b strings.Builder
	for i := 0; i < len(input); {
		ch := input[i]
		switch {
		case ch == '"':
			end := scanJSONString(input, i)
			token := input[i:end]
			if jsonStringIsObjectKey(input, end) {
				b.WriteString(colorCyan + token + colorReset)
			} else {
				b.WriteString(colorGreen + token + colorReset)
			}
			i = end
		case isJSONNumberStart(ch):
			end := scanJSONNumber(input, i)
			b.WriteString(colorMagenta + input[i:end] + colorReset)
			i = end
		case strings.HasPrefix(input[i:], "true"):
			b.WriteString(colorYellow + "true" + colorReset)
			i += len("true")
		case strings.HasPrefix(input[i:], "false"):
			b.WriteString(colorYellow + "false" + colorReset)
			i += len("false")
		case strings.HasPrefix(input[i:], "null"):
			b.WriteString(colorDim + "null" + colorReset)
			i += len("null")
		case strings.ContainsRune("{}[],:", rune(ch)):
			b.WriteString(colorDim + string(ch) + colorReset)
			i++
		default:
			b.WriteByte(ch)
			i++
		}
	}
	return b.String()
}

func scanJSONString(input string, start int) int {
	escaped := false
	for i := start + 1; i < len(input); i++ {
		if escaped {
			escaped = false
			continue
		}
		switch input[i] {
		case '\\':
			escaped = true
		case '"':
			return i + 1
		}
	}
	return len(input)
}

func jsonStringIsObjectKey(input string, afterString int) bool {
	for i := afterString; i < len(input); i++ {
		switch input[i] {
		case ' ', '\n', '\r', '\t':
			continue
		case ':':
			return true
		default:
			return false
		}
	}
	return false
}

func isJSONNumberStart(ch byte) bool {
	return ch == '-' || (ch >= '0' && ch <= '9')
}

func scanJSONNumber(input string, start int) int {
	i := start
	for i < len(input) {
		ch := input[i]
		if (ch >= '0' && ch <= '9') || ch == '-' || ch == '+' || ch == '.' || ch == 'e' || ch == 'E' {
			i++
			continue
		}
		break
	}
	return i
}

func safeName(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('-')
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "cluster"
	}
	return out
}

func defaultBucketName(accountID, region, name, issuerID string) string {
	return fmt.Sprintf("local-irsa-%s-%s-%s-%s", accountID, region, safeName(name), issuerID)
}

func generateIssuerID() (string, error) {
	randomBytes := make([]byte, 8)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(randomBytes)), nil
}

func validateIssuerID(issuerID string) error {
	if len(issuerID) < 8 {
		return fmt.Errorf("issuerID must be at least 8 characters")
	}
	for _, r := range issuerID {
		if (r >= 'a' && r <= 'z') || (r >= '2' && r <= '7') {
			continue
		}
		return fmt.Errorf("issuerID must contain only lowercase base32 characters")
	}
	return nil
}

func issuerURLForBucket(bucket, region string) string {
	return fmt.Sprintf("https://%s.s3.%s.amazonaws.com", bucket, region)
}

func kindSnippet(issuerURL string) string {
	return fmt.Sprintf(`nodes:
  - role: control-plane
    kubeadmConfigPatches:
      - |
        kind: ClusterConfiguration
        apiServer:
          extraArgs:
            service-account-issuer: "%s"
            service-account-jwks-uri: "%s/keys.json"
`, issuerURL, issuerURL)
}

func buildDiscoveryDocument(issuerURL string) map[string]any {
	return map[string]any{
		"issuer":                                issuerURL,
		"jwks_uri":                              issuerURL + "/keys.json",
		"response_types_supported":              []string{"id_token"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
	}
}

type clusterOIDC struct {
	Discovery map[string]any
	JWKS      []byte
}

func readClusterOIDC(ctx context.Context, kube KubeClient, state State) (clusterOIDC, error) {
	discovery, err := readClusterDiscovery(ctx, kube, state)
	if err != nil {
		return clusterOIDC{}, err
	}
	jwksRaw, _, err := readClusterJWKS(ctx, kube)
	if err != nil {
		return clusterOIDC{}, err
	}
	return clusterOIDC{Discovery: discovery, JWKS: jwksRaw}, nil
}

func readClusterDiscovery(ctx context.Context, kube KubeClient, state State) (map[string]any, error) {
	discoveryRaw, err := kube.Raw(ctx, "/.well-known/openid-configuration")
	if err != nil {
		return nil, err
	}
	var discovery map[string]any
	if err := json.Unmarshal(discoveryRaw, &discovery); err != nil {
		return nil, fmt.Errorf("cluster discovery document is not JSON: %w", err)
	}
	if got, _ := discovery["issuer"].(string); got != state.IssuerURL {
		return nil, fmt.Errorf("cluster issuer mismatch: got %q, want %q", got, state.IssuerURL)
	}
	if got, _ := discovery["jwks_uri"].(string); got != state.IssuerURL+"/keys.json" {
		return nil, fmt.Errorf("cluster jwks_uri mismatch: got %q, want %q", got, state.IssuerURL+"/keys.json")
	}
	return discovery, nil
}

func readClusterJWKS(ctx context.Context, kube KubeClient) ([]byte, int, error) {
	jwksRaw, err := kube.Raw(ctx, "/openid/v1/jwks")
	if err != nil {
		return nil, 0, err
	}
	keyCount, err := validateJWKS(jwksRaw)
	if err != nil {
		return nil, 0, err
	}
	return jwksRaw, keyCount, nil
}

func validateJWKS(raw []byte) (int, error) {
	var doc struct {
		Keys []json.RawMessage `json:"keys"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return 0, fmt.Errorf("JWKS is not JSON: %w", err)
	}
	if len(doc.Keys) == 0 {
		return 0, errors.New("JWKS has no keys")
	}
	return len(doc.Keys), nil
}

func normalizeJSON(raw []byte) ([]byte, error) {
	var v any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return marshalPretty(v)
}

func marshalPretty(v any) ([]byte, error) {
	body, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(body, '\n'), nil
}

func upsertBinding(bindings []Binding, next Binding) []Binding {
	out := bindings[:0]
	for _, binding := range bindings {
		if binding.Namespace == next.Namespace && binding.ServiceAccount == next.ServiceAccount {
			continue
		}
		out = append(out, binding)
	}
	out = append(out, next)
	sort.Slice(out, func(i, j int) bool {
		left := out[i].Namespace + "/" + out[i].ServiceAccount
		right := out[j].Namespace + "/" + out[j].ServiceAccount
		return left < right
	})
	return out
}

func chooseProfile(flagValue, stateValue string) string {
	if flagValue != "" {
		return flagValue
	}
	return stateValue
}

func oidcProviderARN(state State) string {
	return fmt.Sprintf("arn:aws:iam::%s:oidc-provider/%s", state.AccountID, issuerHostPath(state.IssuerURL))
}

func issuerHostPath(issuerURL string) string {
	return strings.TrimPrefix(issuerURL, "https://")
}

func trustPolicy(state State, providerARN string, binding Binding) ([]byte, error) {
	subject := fmt.Sprintf("system:serviceaccount:%s:%s", binding.Namespace, binding.ServiceAccount)
	conditionKeyPrefix := issuerHostPath(state.IssuerURL)
	policy := map[string]any{
		"Version": "2012-10-17",
		"Statement": []map[string]any{
			{
				"Effect": "Allow",
				"Principal": map[string]string{
					"Federated": providerARN,
				},
				"Action": "sts:AssumeRoleWithWebIdentity",
				"Condition": map[string]any{
					"StringEquals": map[string]string{
						conditionKeyPrefix + ":aud": defaultTokenAudience,
						conditionKeyPrefix + ":sub": subject,
					},
				},
			},
		},
	}
	return json.Marshal(policy)
}

func decodeIAMPolicyDocument(document string) (string, error) {
	decoded, err := url.QueryUnescape(document)
	if err != nil {
		return "", err
	}
	return decoded, nil
}
