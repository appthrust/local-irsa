package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/smithy-go"
)

type AWSOptions struct {
	Region  string
	Profile string
}

type AWSFactory interface {
	New(context.Context, AWSOptions) (AWSClient, error)
}

type AWSClient interface {
	Region() string
	AccountID(context.Context) (string, error)
	EnsureIssuer(context.Context, State, []byte, []byte) error
	CheckIssuerObjects(context.Context, State, []byte, []byte) error
	EnsureOIDCProvider(context.Context, State) (string, error)
	CheckOIDCProvider(context.Context, State) error
	EnsureRole(context.Context, State, Binding) (string, error)
	EnsureDemoPolicy(context.Context, State, DemoPolicyRequest) (DemoPolicyDetails, error)
	AssumeRoleWithWebIdentity(context.Context, string, string, int32) (string, error)
	DeleteDemoPolicy(context.Context, State, DemoPolicyRequest) (bool, error)
	CleanupRole(context.Context, State, Binding) (bool, error)
	CleanupProvider(context.Context, State) (bool, error)
	CleanupIssuer(context.Context, State, bool) (bool, error)
}

type DemoPolicyRequest struct {
	PolicyName string
	RoleName   string
	Document   []byte
	Tags       map[string]string
}

type DemoPolicyDetails struct {
	Status           string
	PolicyName       string
	PolicyARN        string
	AccountID        string
	RoleName         string
	DefaultVersionID string
	Document         []byte
	Tags             map[string]string
}

type ActualAWSFactory struct{}

func (ActualAWSFactory) New(ctx context.Context, opts AWSOptions) (AWSClient, error) {
	loadOptions := []func(*config.LoadOptions) error{}
	if opts.Region != "" {
		loadOptions = append(loadOptions, config.WithRegion(opts.Region))
	}
	if opts.Profile != "" {
		loadOptions = append(loadOptions, config.WithSharedConfigProfile(opts.Profile))
	}
	cfg, err := config.LoadDefaultConfig(ctx, loadOptions...)
	if err != nil {
		return nil, err
	}
	if cfg.Region == "" {
		return nil, errors.New("AWS region could not be resolved")
	}
	return &actualAWS{
		cfg:    cfg,
		region: cfg.Region,
		sts:    sts.NewFromConfig(cfg),
		s3:     s3.NewFromConfig(cfg),
		iam:    iam.NewFromConfig(cfg),
	}, nil
}

type actualAWS struct {
	cfg    aws.Config
	region string
	sts    *sts.Client
	s3     *s3.Client
	iam    *iam.Client
}

func (a *actualAWS) Region() string {
	return a.region
}

func (a *actualAWS) AccountID(ctx context.Context) (string, error) {
	out, err := a.sts.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return "", err
	}
	return aws.ToString(out.Account), nil
}

func (a *actualAWS) EnsureIssuer(ctx context.Context, state State, discovery, jwks []byte) error {
	if err := a.ensureBucket(ctx, state); err != nil {
		return err
	}
	if err := a.configurePublicAccessBlock(ctx, state); err != nil {
		return err
	}
	if err := a.putIssuerObjects(ctx, state, discovery, jwks); err != nil {
		return err
	}
	return a.putBucketPolicy(ctx, state)
}

func (a *actualAWS) CheckIssuerObjects(ctx context.Context, state State, discovery, jwks []byte) error {
	actualDiscovery, err := a.getS3Object(ctx, state, ".well-known/openid-configuration")
	if err != nil {
		return err
	}
	actualJWKS, err := a.getS3Object(ctx, state, "keys.json")
	if err != nil {
		return err
	}
	normalizedDiscovery, err := normalizeJSON(actualDiscovery)
	if err != nil {
		return fmt.Errorf("S3 discovery document is not JSON: %w", err)
	}
	normalizedJWKS, err := normalizeJSON(actualJWKS)
	if err != nil {
		return fmt.Errorf("S3 JWKS is not JSON: %w", err)
	}
	if !bytes.Equal(normalizedDiscovery, discovery) {
		return errors.New("S3 discovery document does not match current cluster")
	}
	if !bytes.Equal(normalizedJWKS, jwks) {
		return errors.New("S3 JWKS does not match current cluster")
	}
	return nil
}

func (a *actualAWS) EnsureOIDCProvider(ctx context.Context, state State) (string, error) {
	providerARN := oidcProviderARN(state)
	out, err := a.iam.GetOpenIDConnectProvider(ctx, &iam.GetOpenIDConnectProviderInput{
		OpenIDConnectProviderArn: aws.String(providerARN),
	})
	if err != nil {
		if isNoSuchEntity(err) {
			_, err := a.iam.CreateOpenIDConnectProvider(ctx, &iam.CreateOpenIDConnectProviderInput{
				Url:          aws.String(state.IssuerURL),
				ClientIDList: []string{defaultTokenAudience},
				Tags:         iamTags(ownershipTags(state)),
			})
			if err != nil {
				return "", err
			}
			return providerARN, nil
		}
		return "", err
	}
	if !tagsOwned(iamTagMap(out.Tags), state) {
		return "", fmt.Errorf("IAM OIDC Provider %s is not owned by local-irsa cluster %s", providerARN, state.Name)
	}
	if !containsString(out.ClientIDList, defaultTokenAudience) {
		if _, err := a.iam.AddClientIDToOpenIDConnectProvider(ctx, &iam.AddClientIDToOpenIDConnectProviderInput{
			OpenIDConnectProviderArn: aws.String(providerARN),
			ClientID:                 aws.String(defaultTokenAudience),
		}); err != nil {
			return "", err
		}
	}
	return providerARN, nil
}

func (a *actualAWS) CheckOIDCProvider(ctx context.Context, state State) error {
	providerARN := oidcProviderARN(state)
	out, err := a.iam.GetOpenIDConnectProvider(ctx, &iam.GetOpenIDConnectProviderInput{
		OpenIDConnectProviderArn: aws.String(providerARN),
	})
	if err != nil {
		if isNoSuchEntity(err) {
			return fmt.Errorf("IAM OIDC Provider %s does not exist", providerARN)
		}
		return err
	}
	if !tagsOwned(iamTagMap(out.Tags), state) {
		return fmt.Errorf("IAM OIDC Provider %s is not owned by local-irsa cluster %s", providerARN, state.Name)
	}
	if !containsString(out.ClientIDList, defaultTokenAudience) {
		return fmt.Errorf("IAM OIDC Provider %s does not allow client ID %s", providerARN, defaultTokenAudience)
	}
	return nil
}

func (a *actualAWS) EnsureRole(ctx context.Context, state State, binding Binding) (string, error) {
	providerARN, err := a.EnsureOIDCProvider(ctx, state)
	if err != nil {
		return "", err
	}
	policy, err := trustPolicy(state, providerARN, binding)
	if err != nil {
		return "", err
	}
	roleOut, err := a.iam.GetRole(ctx, &iam.GetRoleInput{RoleName: aws.String(binding.RoleName)})
	if err != nil {
		if !isNoSuchEntity(err) {
			return "", err
		}
		createOut, err := a.iam.CreateRole(ctx, &iam.CreateRoleInput{
			RoleName:                 aws.String(binding.RoleName),
			AssumeRolePolicyDocument: aws.String(string(policy)),
			Tags: iamTags(mergeTags(ownershipTags(state), map[string]string{
				saNamespaceTagKey: binding.Namespace,
				saNameTagKey:      binding.ServiceAccount,
			})),
		})
		if err != nil {
			return "", err
		}
		for _, policyARN := range binding.PolicyARNs {
			if err := a.attachRolePolicy(ctx, binding.RoleName, policyARN); err != nil {
				return "", err
			}
		}
		return aws.ToString(createOut.Role.Arn), nil
	}
	roleTags, err := a.roleTags(ctx, binding.RoleName)
	if err != nil {
		return "", err
	}
	if !tagsOwned(roleTags, state) {
		return "", fmt.Errorf("IAM Role %s is not owned by local-irsa cluster %s", binding.RoleName, state.Name)
	}
	if _, err := a.iam.UpdateAssumeRolePolicy(ctx, &iam.UpdateAssumeRolePolicyInput{
		RoleName:       aws.String(binding.RoleName),
		PolicyDocument: aws.String(string(policy)),
	}); err != nil {
		return "", err
	}
	for _, policyARN := range binding.PolicyARNs {
		if err := a.attachRolePolicy(ctx, binding.RoleName, policyARN); err != nil {
			return "", err
		}
	}
	return aws.ToString(roleOut.Role.Arn), nil
}

func (a *actualAWS) EnsureDemoPolicy(ctx context.Context, state State, request DemoPolicyRequest) (DemoPolicyDetails, error) {
	policyARN := customerManagedPolicyARN(state, request.PolicyName)
	details := DemoPolicyDetails{
		PolicyName: request.PolicyName,
		PolicyARN:  policyARN,
		AccountID:  state.AccountID,
		RoleName:   request.RoleName,
		Document:   append([]byte(nil), request.Document...),
		Tags:       copyStringMap(request.Tags),
	}

	policyOut, err := a.iam.GetPolicy(ctx, &iam.GetPolicyInput{PolicyArn: aws.String(policyARN)})
	if err != nil {
		if !isNoSuchEntity(err) {
			return DemoPolicyDetails{}, err
		}
		createOut, err := a.iam.CreatePolicy(ctx, &iam.CreatePolicyInput{
			PolicyName:     aws.String(request.PolicyName),
			PolicyDocument: aws.String(string(request.Document)),
			Tags:           iamTags(request.Tags),
		})
		if err != nil {
			return DemoPolicyDetails{}, err
		}
		details.Status = "created"
		if createOut.Policy != nil {
			if arn := aws.ToString(createOut.Policy.Arn); arn != "" {
				details.PolicyARN = arn
			}
			details.DefaultVersionID = aws.ToString(createOut.Policy.DefaultVersionId)
		}
		if details.DefaultVersionID == "" {
			details.DefaultVersionID = "v1"
		}
		return details, nil
	}

	tags, err := a.policyTags(ctx, policyARN)
	if err != nil {
		return DemoPolicyDetails{}, err
	}
	if !demoPolicyTagsMatch(tags, state) {
		return DemoPolicyDetails{}, fmt.Errorf("IAM Policy %s is not owned by local-irsa demo for cluster %s", policyARN, state.Name)
	}
	if policyOut.Policy == nil {
		return DemoPolicyDetails{}, fmt.Errorf("IAM Policy %s was empty", policyARN)
	}
	defaultVersionID := aws.ToString(policyOut.Policy.DefaultVersionId)
	if defaultVersionID == "" {
		return DemoPolicyDetails{}, fmt.Errorf("IAM Policy %s has no default version", policyARN)
	}
	existingDocument, err := a.policyVersionDocument(ctx, policyARN, defaultVersionID)
	if err != nil {
		return DemoPolicyDetails{}, err
	}
	normalizedExisting, err := normalizeJSON([]byte(existingDocument))
	if err != nil {
		return DemoPolicyDetails{}, fmt.Errorf("existing IAM Policy %s document is not JSON: %w", policyARN, err)
	}
	normalizedExpected, err := normalizeJSON(request.Document)
	if err != nil {
		return DemoPolicyDetails{}, err
	}
	if !bytes.Equal(normalizedExisting, normalizedExpected) {
		return DemoPolicyDetails{}, fmt.Errorf("IAM Policy %s document does not match the local-irsa demo policy", policyARN)
	}
	details.Status = "reused"
	details.DefaultVersionID = defaultVersionID
	return details, nil
}

func (a *actualAWS) AssumeRoleWithWebIdentity(ctx context.Context, roleARN, token string, duration int32) (string, error) {
	out, err := a.sts.AssumeRoleWithWebIdentity(ctx, &sts.AssumeRoleWithWebIdentityInput{
		RoleArn:          aws.String(roleARN),
		RoleSessionName:  aws.String("local-irsa-doctor"),
		WebIdentityToken: aws.String(token),
		DurationSeconds:  aws.Int32(duration),
	})
	if err != nil {
		return "", err
	}
	if out.AssumedRoleUser == nil {
		return "", errors.New("AssumeRoleWithWebIdentity returned no assumed role user")
	}
	return aws.ToString(out.AssumedRoleUser.Arn), nil
}

func (a *actualAWS) DeleteDemoPolicy(ctx context.Context, state State, request DemoPolicyRequest) (bool, error) {
	policyARN := customerManagedPolicyARN(state, request.PolicyName)
	_, err := a.iam.GetPolicy(ctx, &iam.GetPolicyInput{PolicyArn: aws.String(policyARN)})
	if err != nil {
		if isNoSuchEntity(err) {
			return false, nil
		}
		return false, err
	}
	tags, err := a.policyTags(ctx, policyARN)
	if err != nil {
		return false, err
	}
	if !demoPolicyTagsMatch(tags, state) {
		return false, fmt.Errorf("IAM Policy %s is not owned by local-irsa demo for cluster %s", policyARN, state.Name)
	}
	_, err = a.iam.DeletePolicy(ctx, &iam.DeletePolicyInput{PolicyArn: aws.String(policyARN)})
	if err != nil {
		if isDeleteConflict(err) {
			return false, fmt.Errorf("IAM Policy %s is still attached; run local-irsa unbind --name %s --namespace default --service-account local-irsa-demo, or local-irsa down --name %s for full cleanup, before deleting the demo policy", policyARN, state.Name, state.Name)
		}
		return false, err
	}
	return true, nil
}

func (a *actualAWS) CleanupRole(ctx context.Context, state State, binding Binding) (bool, error) {
	tags, err := a.roleTags(ctx, binding.RoleName)
	if err != nil {
		if isNoSuchEntity(err) {
			return true, nil
		}
		return false, err
	}
	if !tagsOwned(tags, state) {
		return false, nil
	}
	for _, policyARN := range binding.PolicyARNs {
		if _, err := a.iam.DetachRolePolicy(ctx, &iam.DetachRolePolicyInput{
			RoleName:  aws.String(binding.RoleName),
			PolicyArn: aws.String(policyARN),
		}); err != nil && !isNoSuchEntity(err) {
			return false, err
		}
	}
	_, err = a.iam.DeleteRole(ctx, &iam.DeleteRoleInput{RoleName: aws.String(binding.RoleName)})
	if err != nil && !isNoSuchEntity(err) {
		return false, err
	}
	return true, nil
}

func (a *actualAWS) CleanupProvider(ctx context.Context, state State) (bool, error) {
	providerARN := oidcProviderARN(state)
	out, err := a.iam.GetOpenIDConnectProvider(ctx, &iam.GetOpenIDConnectProviderInput{
		OpenIDConnectProviderArn: aws.String(providerARN),
	})
	if err != nil {
		if isNoSuchEntity(err) {
			return true, nil
		}
		return false, err
	}
	if !tagsOwned(iamTagMap(out.Tags), state) {
		return false, nil
	}
	_, err = a.iam.DeleteOpenIDConnectProvider(ctx, &iam.DeleteOpenIDConnectProviderInput{
		OpenIDConnectProviderArn: aws.String(providerARN),
	})
	if err != nil && !isNoSuchEntity(err) {
		return false, err
	}
	return true, nil
}

func (a *actualAWS) CleanupIssuer(ctx context.Context, state State, deleteBucket bool) (bool, error) {
	tags, err := a.bucketTags(ctx, state)
	if err != nil {
		if isNoSuchBucket(err) {
			return true, nil
		}
		return false, err
	}
	if !tagsOwned(tags, state) {
		return false, nil
	}
	for _, key := range []string{".well-known/openid-configuration", "keys.json"} {
		if _, err := a.s3.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket:              aws.String(state.Bucket),
			Key:                 aws.String(key),
			ExpectedBucketOwner: aws.String(state.AccountID),
		}); err != nil && !isNoSuchBucket(err) {
			return false, err
		}
	}
	if _, err := a.s3.DeleteBucketPolicy(ctx, &s3.DeleteBucketPolicyInput{
		Bucket:              aws.String(state.Bucket),
		ExpectedBucketOwner: aws.String(state.AccountID),
	}); err != nil && !isNoSuchBucket(err) && !isNoSuchBucketPolicy(err) {
		return false, err
	}
	if deleteBucket {
		if _, err := a.s3.DeleteBucket(ctx, &s3.DeleteBucketInput{
			Bucket:              aws.String(state.Bucket),
			ExpectedBucketOwner: aws.String(state.AccountID),
		}); err != nil && !isNoSuchBucket(err) {
			return false, err
		}
	}
	return true, nil
}

func (a *actualAWS) ensureBucket(ctx context.Context, state State) error {
	_, err := a.s3.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket:              aws.String(state.Bucket),
		ExpectedBucketOwner: aws.String(state.AccountID),
	})
	if err == nil {
		tags, err := a.bucketTags(ctx, state)
		if err != nil {
			return err
		}
		if !tagsOwned(tags, state) {
			return fmt.Errorf("S3 bucket %s is not owned by local-irsa cluster %s", state.Bucket, state.Name)
		}
		return nil
	}
	if !isNoSuchBucket(err) && !isNotFound(err) {
		return err
	}
	input := &s3.CreateBucketInput{Bucket: aws.String(state.Bucket)}
	if a.region != "us-east-1" {
		input.CreateBucketConfiguration = &s3types.CreateBucketConfiguration{
			LocationConstraint: s3types.BucketLocationConstraint(a.region),
		}
	}
	if _, err := a.s3.CreateBucket(ctx, input); err != nil {
		return err
	}
	_, err = a.s3.PutBucketTagging(ctx, &s3.PutBucketTaggingInput{
		Bucket:              aws.String(state.Bucket),
		ExpectedBucketOwner: aws.String(state.AccountID),
		Tagging: &s3types.Tagging{
			TagSet: s3Tags(ownershipTags(state)),
		},
	})
	return err
}

func (a *actualAWS) configurePublicAccessBlock(ctx context.Context, state State) error {
	_, err := a.s3.PutPublicAccessBlock(ctx, &s3.PutPublicAccessBlockInput{
		Bucket:              aws.String(state.Bucket),
		ExpectedBucketOwner: aws.String(state.AccountID),
		PublicAccessBlockConfiguration: &s3types.PublicAccessBlockConfiguration{
			BlockPublicAcls:       aws.Bool(true),
			IgnorePublicAcls:      aws.Bool(true),
			BlockPublicPolicy:     aws.Bool(false),
			RestrictPublicBuckets: aws.Bool(false),
		},
	})
	return err
}

func (a *actualAWS) putIssuerObjects(ctx context.Context, state State, discovery, jwks []byte) error {
	for _, object := range []struct {
		key  string
		body []byte
	}{
		{key: ".well-known/openid-configuration", body: discovery},
		{key: "keys.json", body: jwks},
	} {
		if _, err := a.s3.PutObject(ctx, &s3.PutObjectInput{
			Bucket:              aws.String(state.Bucket),
			Key:                 aws.String(object.key),
			Body:                bytes.NewReader(object.body),
			ContentType:         aws.String("application/json"),
			ExpectedBucketOwner: aws.String(state.AccountID),
		}); err != nil {
			return err
		}
	}
	return nil
}

func (a *actualAWS) putBucketPolicy(ctx context.Context, state State) error {
	policy := map[string]any{
		"Version": "2012-10-17",
		"Statement": []map[string]any{
			{
				"Effect":    "Allow",
				"Principal": "*",
				"Action":    "s3:GetObject",
				"Resource": []string{
					fmt.Sprintf("arn:aws:s3:::%s/.well-known/openid-configuration", state.Bucket),
					fmt.Sprintf("arn:aws:s3:::%s/keys.json", state.Bucket),
				},
			},
		},
	}
	body, err := json.Marshal(policy)
	if err != nil {
		return err
	}
	_, err = a.s3.PutBucketPolicy(ctx, &s3.PutBucketPolicyInput{
		Bucket:              aws.String(state.Bucket),
		Policy:              aws.String(string(body)),
		ExpectedBucketOwner: aws.String(state.AccountID),
	})
	return err
}

func (a *actualAWS) getS3Object(ctx context.Context, state State, key string) ([]byte, error) {
	out, err := a.s3.GetObject(ctx, &s3.GetObjectInput{
		Bucket:              aws.String(state.Bucket),
		Key:                 aws.String(key),
		ExpectedBucketOwner: aws.String(state.AccountID),
	})
	if err != nil {
		return nil, err
	}
	defer out.Body.Close()
	return io.ReadAll(out.Body)
}

func (a *actualAWS) bucketTags(ctx context.Context, state State) (map[string]string, error) {
	out, err := a.s3.GetBucketTagging(ctx, &s3.GetBucketTaggingInput{
		Bucket:              aws.String(state.Bucket),
		ExpectedBucketOwner: aws.String(state.AccountID),
	})
	if err != nil {
		if isNoSuchTagSet(err) {
			return map[string]string{}, nil
		}
		return nil, err
	}
	tags := map[string]string{}
	for _, tag := range out.TagSet {
		tags[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
	}
	return tags, nil
}

func (a *actualAWS) roleTags(ctx context.Context, roleName string) (map[string]string, error) {
	paginator := iam.NewListRoleTagsPaginator(a.iam, &iam.ListRoleTagsInput{RoleName: aws.String(roleName)})
	tags := map[string]string{}
	for paginator.HasMorePages() {
		out, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, tag := range out.Tags {
			tags[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
		}
	}
	return tags, nil
}

func (a *actualAWS) attachRolePolicy(ctx context.Context, roleName, policyARN string) error {
	_, err := a.iam.AttachRolePolicy(ctx, &iam.AttachRolePolicyInput{
		RoleName:  aws.String(roleName),
		PolicyArn: aws.String(policyARN),
	})
	return err
}

func (a *actualAWS) policyTags(ctx context.Context, policyARN string) (map[string]string, error) {
	paginator := iam.NewListPolicyTagsPaginator(a.iam, &iam.ListPolicyTagsInput{PolicyArn: aws.String(policyARN)})
	tags := map[string]string{}
	for paginator.HasMorePages() {
		out, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, tag := range out.Tags {
			tags[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
		}
	}
	return tags, nil
}

func (a *actualAWS) policyVersionDocument(ctx context.Context, policyARN, versionID string) (string, error) {
	out, err := a.iam.GetPolicyVersion(ctx, &iam.GetPolicyVersionInput{
		PolicyArn: aws.String(policyARN),
		VersionId: aws.String(versionID),
	})
	if err != nil {
		return "", err
	}
	if out.PolicyVersion == nil {
		return "", fmt.Errorf("IAM Policy %s version %s was empty", policyARN, versionID)
	}
	return decodeIAMPolicyDocument(aws.ToString(out.PolicyVersion.Document))
}

func ownershipTags(state State) map[string]string {
	return map[string]string{
		managedByTagKey: managedByTagValue,
		clusterTagKey:   state.Name,
	}
}

func mergeTags(left, right map[string]string) map[string]string {
	out := make(map[string]string, len(left)+len(right))
	for k, v := range left {
		out[k] = v
	}
	for k, v := range right {
		out[k] = v
	}
	return out
}

func tagsOwned(tags map[string]string, state State) bool {
	return tags[managedByTagKey] == managedByTagValue && tags[clusterTagKey] == state.Name
}

func demoPolicyTagsMatch(tags map[string]string, state State) bool {
	return tagsOwned(tags, state) && tags[demoPurposeTagKey] == demoPurposeTagValue
}

func copyStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func s3Tags(tags map[string]string) []s3types.Tag {
	out := make([]s3types.Tag, 0, len(tags))
	for key, value := range tags {
		out = append(out, s3types.Tag{Key: aws.String(key), Value: aws.String(value)})
	}
	return out
}

func iamTags(tags map[string]string) []iamtypes.Tag {
	out := make([]iamtypes.Tag, 0, len(tags))
	for key, value := range tags {
		out = append(out, iamtypes.Tag{Key: aws.String(key), Value: aws.String(value)})
	}
	return out
}

func iamTagMap(tags []iamtypes.Tag) map[string]string {
	out := map[string]string{}
	for _, tag := range tags {
		out[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
	}
	return out
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func isNoSuchEntity(err error) bool {
	var apiErr smithy.APIError
	return errors.As(err, &apiErr) && (apiErr.ErrorCode() == "NoSuchEntity" || apiErr.ErrorCode() == "NoSuchEntityException")
}

func isNoSuchBucket(err error) bool {
	var apiErr smithy.APIError
	return errors.As(err, &apiErr) && (apiErr.ErrorCode() == "NoSuchBucket" || apiErr.ErrorCode() == "NoSuchBucketException")
}

func isNoSuchBucketPolicy(err error) bool {
	var apiErr smithy.APIError
	return errors.As(err, &apiErr) && strings.Contains(apiErr.ErrorCode(), "NoSuchBucketPolicy")
}

func isNoSuchTagSet(err error) bool {
	var apiErr smithy.APIError
	return errors.As(err, &apiErr) && apiErr.ErrorCode() == "NoSuchTagSet"
}

func isNotFound(err error) bool {
	var apiErr smithy.APIError
	return errors.As(err, &apiErr) && apiErr.ErrorCode() == "NotFound"
}

func isDeleteConflict(err error) bool {
	var apiErr smithy.APIError
	return errors.As(err, &apiErr) && apiErr.ErrorCode() == "DeleteConflict"
}
