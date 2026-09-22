package store

import (
	"context"
	"fmt"
	"net/url"
)

// platformBase is the module-lifecycle base path on the Developer Store API.
const platformBase = "/api/developer-store"

// modulePath returns the product-scoped path for a module identifier (id or token).
func modulePath(moduleID string, suffix string) string {
	base := fmt.Sprintf("%s/modules/%s", platformBase, url.PathEscape(moduleID))
	if suffix == "" {
		return base
	}
	return base + suffix
}

// --- Read models -----------------------------------------------------------

// KnowledgeArticle is a documentation entry of a module.
type KnowledgeArticle struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	Slug           string `json:"slug"`
	Kind           string `json:"kind"`
	Status         string `json:"status"`
	ReadingMinutes int    `json:"readingMinutes"`
	UpdatedAt      string `json:"updatedAt"`
}

// Workflow is a CI/CD workflow declaration.
type Workflow struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Source    string `json:"source"`
	Trigger   string `json:"trigger"`
	Status    string `json:"status"`
	LastRunAt string `json:"lastRunAt"`
	RunCount  int    `json:"runCount"`
}

// BuildChannel is a distribution channel state.
type BuildChannel struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	VersionString   string `json:"versionString"`
	Status          string `json:"status"`
	UpdateCount     int    `json:"updateCount"`
	RollbackAllowed bool   `json:"rollbackAllowed"`
}

// PlatformSupport is a module platform capability.
type PlatformSupport struct {
	Platform     string   `json:"platform"`
	Supported    bool     `json:"supported"`
	Modes        []string `json:"modes"`
	Os           []string `json:"os"`
	MinOsVersion string   `json:"minOsVersion"`
}

// ModuleRequirement is a module dependency.
type ModuleRequirement struct {
	ID           string `json:"id"`
	ModuleID     string `json:"moduleId"`
	Name         string `json:"name"`
	VersionRange string `json:"versionRange"`
	Kind         string `json:"kind"`
	Reason       string `json:"reason"`
}

// SigningKey is a module signing key.
type SigningKey struct {
	ID        string `json:"id"`
	KeyID     string `json:"keyId"`
	Algorithm string `json:"algorithm"`
	Status    string `json:"status"`
	CreatedAt string `json:"createdAt"`
}

// Accreditation is an account-level accreditation.
type Accreditation struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	UpdatedAt string `json:"updatedAt"`
}

// EnvironmentVariable is a (masked) environment variable.
type EnvironmentVariable struct {
	ID           string   `json:"id"`
	Key          string   `json:"key"`
	Environments []string `json:"environments"`
	Visibility   string   `json:"visibility"`
	ProductID    string   `json:"productId"`
}

// GithubConnection is the GitHub repository binding.
type GithubConnection struct {
	ID                 string `json:"id"`
	Repository         string `json:"repository"`
	DefaultBranch      string `json:"defaultBranch"`
	WorkflowPath       string `json:"workflowPath"`
	LastWorkflowStatus string `json:"lastWorkflowStatus"`
	Connected          bool   `json:"connected"`
}

// ObserverMetric is a health metric.
type ObserverMetric struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Value string `json:"value"`
	Trend string `json:"trend"`
	Hint  string `json:"hint"`
}

// BetaCrash is a crash report entry.
type BetaCrash struct {
	ID       string `json:"id"`
	Platform string `json:"platform"`
	Summary  string `json:"summary"`
	Count    int    `json:"count"`
}

// ObserverOverview is the observability view of a module.
type ObserverOverview struct {
	ProductID string           `json:"productId"`
	Metrics   []ObserverMetric `json:"metrics"`
	Crashes   []BetaCrash      `json:"crashes"`
}

// UsagePoint is a weekly usage bucket.
type UsagePoint struct {
	Label         string `json:"label"`
	Installations int    `json:"installations"`
	Activations   int    `json:"activations"`
}

// UsageOverview is the usage series of a module.
type UsageOverview struct {
	ProductID string       `json:"productId"`
	Series    []UsagePoint `json:"series"`
	Totals    struct {
		Installations       int `json:"installations"`
		Activations         int `json:"activations"`
		ActiveOrganizations int `json:"activeOrganizations"`
	} `json:"totals"`
}

// DevBuild is a development build of a module.
type DevBuild struct {
	ID             string `json:"id"`
	RuntimeVersion string `json:"runtimeVersion"`
	Platform       string `json:"platform"`
	ArtifactState  string `json:"artifactState"`
	CanInstall     bool   `json:"canInstall"`
	CreatedAt      string `json:"createdAt"`
}

// Fingerprint is a runtime fingerprint definition of a module.
type Fingerprint struct {
	ID              string   `json:"id"`
	Hash            string   `json:"hash"`
	RuntimeVersions []string `json:"runtimeVersions"`
	Channels        []string `json:"channels"`
	CreatedAt       string   `json:"createdAt"`
}

// CacheEntry is an execution cache entry of a module.
type CacheEntry struct {
	ID           string `json:"id"`
	Key          string `json:"key"`
	SizeBytes    int64  `json:"sizeBytes"`
	TTLSeconds   int    `json:"ttlSeconds"`
	LastAccessAt string `json:"lastAccessAt"`
	Hits         int    `json:"hits"`
}

// --- Client methods --------------------------------------------------------

// ListKnowledgeArticles returns the documentation of a module.
func (c *Client) ListKnowledgeArticles(ctx context.Context, moduleID string) ([]KnowledgeArticle, error) {
	var out []KnowledgeArticle
	if err := c.http().Do(ctx, "GET", modulePath(moduleID, "/knowledge"), nil, &out); err != nil {
		return nil, fmt.Errorf("failed to fetch knowledge articles: %w", err)
	}
	return out, nil
}

// ListWorkflows returns the CI/CD workflows of a module.
func (c *Client) ListWorkflows(ctx context.Context, moduleID string) ([]Workflow, error) {
	var out []Workflow
	if err := c.http().Do(ctx, "GET", modulePath(moduleID, "/workflows"), nil, &out); err != nil {
		return nil, fmt.Errorf("failed to fetch workflows: %w", err)
	}
	return out, nil
}

// RunWorkflow triggers a workflow execution.
func (c *Client) RunWorkflow(ctx context.Context, moduleID, workflowID string) (*Workflow, error) {
	var out Workflow
	if err := c.http().Do(ctx, "POST", modulePath(moduleID, "/workflows/"+url.PathEscape(workflowID)+"/run"), nil, &out); err != nil {
		return nil, fmt.Errorf("failed to run workflow: %w", err)
	}
	return &out, nil
}

// ListBuildChannels returns the distribution channels of a module.
func (c *Client) ListBuildChannels(ctx context.Context, moduleID string) ([]BuildChannel, error) {
	var out []BuildChannel
	if err := c.http().Do(ctx, "GET", modulePath(moduleID, "/channels"), nil, &out); err != nil {
		return nil, fmt.Errorf("failed to fetch build channels: %w", err)
	}
	return out, nil
}

// PublishChannel publishes a version on a distribution channel.
func (c *Client) PublishChannel(ctx context.Context, moduleID, channel, version string) (*BuildChannel, error) {
	body := map[string]string{"versionString": version}
	var out BuildChannel
	path := modulePath(moduleID, "/channels/"+url.PathEscape(channel)+"/publish")
	if err := c.http().Do(ctx, "POST", path, body, &out); err != nil {
		return nil, fmt.Errorf("failed to publish on channel %s: %w", channel, err)
	}
	return &out, nil
}

// GetModulePlatforms returns the declared platform support of a module.
func (c *Client) GetModulePlatforms(ctx context.Context, moduleID string) ([]PlatformSupport, error) {
	var out []PlatformSupport
	if err := c.http().Do(ctx, "GET", modulePath(moduleID, "/platforms"), nil, &out); err != nil {
		return nil, fmt.Errorf("failed to fetch platforms: %w", err)
	}
	return out, nil
}

// ListDevBuilds returns the development builds of a module.
func (c *Client) ListDevBuilds(ctx context.Context, moduleID string) ([]DevBuild, error) {
	var out []DevBuild
	if err := c.http().Do(ctx, "GET", modulePath(moduleID, "/dev-builds"), nil, &out); err != nil {
		return nil, fmt.Errorf("failed to fetch dev builds: %w", err)
	}
	return out, nil
}

// ListFingerprints returns the runtime fingerprints of a module.
func (c *Client) ListFingerprints(ctx context.Context, moduleID string) ([]Fingerprint, error) {
	var out []Fingerprint
	if err := c.http().Do(ctx, "GET", modulePath(moduleID, "/fingerprints"), nil, &out); err != nil {
		return nil, fmt.Errorf("failed to fetch fingerprints: %w", err)
	}
	return out, nil
}

// ListCaches returns the execution cache entries of a module.
func (c *Client) ListCaches(ctx context.Context, moduleID string) ([]CacheEntry, error) {
	var out []CacheEntry
	if err := c.http().Do(ctx, "GET", modulePath(moduleID, "/caches"), nil, &out); err != nil {
		return nil, fmt.Errorf("failed to fetch caches: %w", err)
	}
	return out, nil
}

// ListRequirements returns the required and optional module dependencies.
func (c *Client) ListRequirements(ctx context.Context, moduleID string) ([]ModuleRequirement, error) {
	var out []ModuleRequirement
	if err := c.http().Do(ctx, "GET", modulePath(moduleID, "/requirements"), nil, &out); err != nil {
		return nil, fmt.Errorf("failed to fetch requirements: %w", err)
	}
	return out, nil
}

// ListSigningKeys returns the signing keys of the account.
func (c *Client) ListSigningKeys(ctx context.Context) ([]SigningKey, error) {
	var out []SigningKey
	if err := c.http().Do(ctx, "GET", platformBase+"/signing-keys", nil, &out); err != nil {
		return nil, fmt.Errorf("failed to fetch signing keys: %w", err)
	}
	return out, nil
}

// ListAccreditations returns the account accreditations.
func (c *Client) ListAccreditations(ctx context.Context) ([]Accreditation, error) {
	var out []Accreditation
	if err := c.http().Do(ctx, "GET", platformBase+"/accreditations", nil, &out); err != nil {
		return nil, fmt.Errorf("failed to fetch accreditations: %w", err)
	}
	return out, nil
}

// ListEnvironmentVariables returns the (module or shared) environment variables.
func (c *Client) ListEnvironmentVariables(ctx context.Context, moduleID string) ([]EnvironmentVariable, error) {
	path := platformBase + "/environment-variables"
	if moduleID != "" {
		path += "?moduleId=" + url.QueryEscape(moduleID)
	}
	var out []EnvironmentVariable
	if err := c.http().Do(ctx, "GET", path, nil, &out); err != nil {
		return nil, fmt.Errorf("failed to fetch environment variables: %w", err)
	}
	return out, nil
}

// GetGithubConnection returns the GitHub connection (module or shared).
func (c *Client) GetGithubConnection(ctx context.Context, moduleID string) (*GithubConnection, error) {
	path := platformBase + "/github"
	if moduleID != "" {
		path += "?moduleId=" + url.QueryEscape(moduleID)
	}
	var out *GithubConnection
	if err := c.http().Do(ctx, "GET", path, nil, &out); err != nil {
		return nil, fmt.Errorf("failed to fetch GitHub connection: %w", err)
	}
	return out, nil
}

// ConnectGithub binds a repository to the account or a module.
func (c *Client) ConnectGithub(ctx context.Context, repository, branch, workflowPath, moduleID string) (*GithubConnection, error) {
	body := map[string]string{"repository": repository}
	if branch != "" {
		body["defaultBranch"] = branch
	}
	if workflowPath != "" {
		body["workflowPath"] = workflowPath
	}
	if moduleID != "" {
		body["productId"] = moduleID
	}
	var out GithubConnection
	if err := c.http().Do(ctx, "POST", platformBase+"/github/connect", body, &out); err != nil {
		return nil, fmt.Errorf("failed to connect GitHub: %w", err)
	}
	return &out, nil
}

// DisconnectGithub removes the GitHub connection (module or shared).
func (c *Client) DisconnectGithub(ctx context.Context, moduleID string) error {
	path := platformBase + "/github"
	if moduleID != "" {
		path += "?moduleId=" + url.QueryEscape(moduleID)
	}
	if err := c.http().Do(ctx, "DELETE", path, nil, nil); err != nil {
		return fmt.Errorf("failed to disconnect GitHub: %w", err)
	}
	return nil
}

// GetObserver returns the observability overview of a module.
func (c *Client) GetObserver(ctx context.Context, moduleID string) (*ObserverOverview, error) {
	var out ObserverOverview
	if err := c.http().Do(ctx, "GET", modulePath(moduleID, "/observer"), nil, &out); err != nil {
		return nil, fmt.Errorf("failed to fetch observer metrics: %w", err)
	}
	return &out, nil
}

// GetUsage returns the usage series of a module.
func (c *Client) GetUsage(ctx context.Context, moduleID string) (*UsageOverview, error) {
	var out UsageOverview
	if err := c.http().Do(ctx, "GET", modulePath(moduleID, "/usage"), nil, &out); err != nil {
		return nil, fmt.Errorf("failed to fetch usage: %w", err)
	}
	return &out, nil
}

// --- Knowledge (write) -----------------------------------------------------

// CreateKnowledgeArticleRequest is the `CreateKnowledgeArticleDto` payload.
type CreateKnowledgeArticleRequest struct {
	Title          string `json:"title"`
	Slug           string `json:"slug"`
	Kind           string `json:"kind,omitempty"`
	Summary        string `json:"summary,omitempty"`
	Content        string `json:"content,omitempty"`
	ReadingMinutes int    `json:"readingMinutes,omitempty"`
	Publish        bool   `json:"publish,omitempty"`
}

// CreateKnowledgeArticle publishes a documentation article.
func (c *Client) CreateKnowledgeArticle(ctx context.Context, moduleID string, body CreateKnowledgeArticleRequest) (*KnowledgeArticle, error) {
	var out KnowledgeArticle
	if err := c.http().Do(ctx, "POST", modulePath(moduleID, "/knowledge"), body, &out); err != nil {
		return nil, fmt.Errorf("failed to create knowledge article: %w", err)
	}
	return &out, nil
}

// PublishKnowledgeArticle flips an article to the published state.
func (c *Client) PublishKnowledgeArticle(ctx context.Context, moduleID, articleID string, publish bool) (*KnowledgeArticle, error) {
	body := map[string]bool{"publish": publish}
	path := modulePath(moduleID, "/knowledge/"+url.PathEscape(articleID))
	var out KnowledgeArticle
	if err := c.http().Do(ctx, "PUT", path, body, &out); err != nil {
		return nil, fmt.Errorf("failed to publish knowledge article: %w", err)
	}
	return &out, nil
}

// DeleteKnowledgeArticle removes a documentation article.
func (c *Client) DeleteKnowledgeArticle(ctx context.Context, moduleID, articleID string) error {
	path := modulePath(moduleID, "/knowledge/"+url.PathEscape(articleID))
	if err := c.http().Do(ctx, "DELETE", path, nil, nil); err != nil {
		return fmt.Errorf("failed to delete knowledge article: %w", err)
	}
	return nil
}

// --- Workflows (write) -----------------------------------------------------

// SaveWorkflowRequest is the create/update payload of a CI/CD workflow.
type SaveWorkflowRequest struct {
	Name         string `json:"name"`
	Source       string `json:"source"`
	Trigger      string `json:"trigger"`
	Status       string `json:"status,omitempty"`
	WorkflowPath string `json:"workflowPath,omitempty"`
}

// CreateWorkflow declares a CI/CD workflow.
func (c *Client) CreateWorkflow(ctx context.Context, moduleID string, body SaveWorkflowRequest) (*Workflow, error) {
	var out Workflow
	if err := c.http().Do(ctx, "POST", modulePath(moduleID, "/workflows"), body, &out); err != nil {
		return nil, fmt.Errorf("failed to create workflow: %w", err)
	}
	return &out, nil
}

// UpdateWorkflow updates a CI/CD workflow declaration.
func (c *Client) UpdateWorkflow(ctx context.Context, moduleID, workflowID string, body SaveWorkflowRequest) (*Workflow, error) {
	path := modulePath(moduleID, "/workflows/"+url.PathEscape(workflowID))
	var out Workflow
	if err := c.http().Do(ctx, "PUT", path, body, &out); err != nil {
		return nil, fmt.Errorf("failed to update workflow: %w", err)
	}
	return &out, nil
}

// DeleteWorkflow removes a CI/CD workflow declaration.
func (c *Client) DeleteWorkflow(ctx context.Context, moduleID, workflowID string) error {
	path := modulePath(moduleID, "/workflows/"+url.PathEscape(workflowID))
	if err := c.http().Do(ctx, "DELETE", path, nil, nil); err != nil {
		return fmt.Errorf("failed to delete workflow: %w", err)
	}
	return nil
}

// --- Channels (write) ------------------------------------------------------

// RollbackChannel restores the previous version of a distribution channel.
func (c *Client) RollbackChannel(ctx context.Context, moduleID, channel string) (*BuildChannel, error) {
	var out BuildChannel
	path := modulePath(moduleID, "/channels/"+url.PathEscape(channel)+"/rollback")
	if err := c.http().Do(ctx, "POST", path, nil, &out); err != nil {
		return nil, fmt.Errorf("failed to rollback channel %s: %w", channel, err)
	}
	return &out, nil
}

// PauseChannel pauses a distribution channel.
func (c *Client) PauseChannel(ctx context.Context, moduleID, channel string) (*BuildChannel, error) {
	var out BuildChannel
	path := modulePath(moduleID, "/channels/"+url.PathEscape(channel)+"/pause")
	if err := c.http().Do(ctx, "POST", path, nil, &out); err != nil {
		return nil, fmt.Errorf("failed to pause channel %s: %w", channel, err)
	}
	return &out, nil
}

// --- Platforms (write) -----------------------------------------------------

// SaveModulePlatforms replaces the whole platform configuration of a module.
func (c *Client) SaveModulePlatforms(ctx context.Context, moduleID string, platforms []PlatformSupport) ([]PlatformSupport, error) {
	body := map[string]any{"platforms": platforms}
	var out []PlatformSupport
	if err := c.http().Do(ctx, "PUT", modulePath(moduleID, "/platforms"), body, &out); err != nil {
		return nil, fmt.Errorf("failed to save platforms: %w", err)
	}
	return out, nil
}

// --- Requirements (write) --------------------------------------------------

// CreateRequirementRequest is the `CreateRequirementDto` payload.
type CreateRequirementRequest struct {
	ModuleID     string `json:"moduleId"`
	Name         string `json:"name"`
	VersionRange string `json:"versionRange"`
	Kind         string `json:"kind,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

// CreateRequirement declares a required or optional module dependency.
func (c *Client) CreateRequirement(ctx context.Context, moduleID string, body CreateRequirementRequest) (*ModuleRequirement, error) {
	var out ModuleRequirement
	if err := c.http().Do(ctx, "POST", modulePath(moduleID, "/requirements"), body, &out); err != nil {
		return nil, fmt.Errorf("failed to create requirement: %w", err)
	}
	return &out, nil
}

// DeleteRequirement removes a module dependency declaration.
func (c *Client) DeleteRequirement(ctx context.Context, moduleID, requirementID string) error {
	path := modulePath(moduleID, "/requirements/"+url.PathEscape(requirementID))
	if err := c.http().Do(ctx, "DELETE", path, nil, nil); err != nil {
		return fmt.Errorf("failed to delete requirement: %w", err)
	}
	return nil
}

// --- Development builds (write) --------------------------------------------

// SaveDevBuildRequest is the `CreateDevBuildDto` payload.
type SaveDevBuildRequest struct {
	RuntimeVersion string `json:"runtimeVersion"`
	Platform       string `json:"platform"`
	ArtifactState  string `json:"artifactState,omitempty"`
}

// CreateDevBuild declares a development build.
func (c *Client) CreateDevBuild(ctx context.Context, moduleID string, body SaveDevBuildRequest) (*DevBuild, error) {
	var out DevBuild
	if err := c.http().Do(ctx, "POST", modulePath(moduleID, "/dev-builds"), body, &out); err != nil {
		return nil, fmt.Errorf("failed to create dev build: %w", err)
	}
	return &out, nil
}

// DeleteDevBuild removes a development build.
func (c *Client) DeleteDevBuild(ctx context.Context, moduleID, buildID string) error {
	path := modulePath(moduleID, "/dev-builds/"+url.PathEscape(buildID))
	if err := c.http().Do(ctx, "DELETE", path, nil, nil); err != nil {
		return fmt.Errorf("failed to delete dev build: %w", err)
	}
	return nil
}

// --- Fingerprints (write) --------------------------------------------------

// SaveFingerprintRequest is the `CreateFingerprintDto` payload.
type SaveFingerprintRequest struct {
	Hash            string   `json:"hash"`
	RuntimeVersions []string `json:"runtimeVersions"`
	Channels        []string `json:"channels"`
}

// CreateFingerprint declares a runtime fingerprint.
func (c *Client) CreateFingerprint(ctx context.Context, moduleID string, body SaveFingerprintRequest) (*Fingerprint, error) {
	var out Fingerprint
	if err := c.http().Do(ctx, "POST", modulePath(moduleID, "/fingerprints"), body, &out); err != nil {
		return nil, fmt.Errorf("failed to create fingerprint: %w", err)
	}
	return &out, nil
}

// DeleteFingerprint removes a runtime fingerprint.
func (c *Client) DeleteFingerprint(ctx context.Context, moduleID, fingerprintID string) error {
	path := modulePath(moduleID, "/fingerprints/"+url.PathEscape(fingerprintID))
	if err := c.http().Do(ctx, "DELETE", path, nil, nil); err != nil {
		return fmt.Errorf("failed to delete fingerprint: %w", err)
	}
	return nil
}

// --- Caches (write) --------------------------------------------------------

// PurgeCaches clears the whole execution cache of a module.
func (c *Client) PurgeCaches(ctx context.Context, moduleID string) error {
	if err := c.http().Do(ctx, "DELETE", modulePath(moduleID, "/caches"), nil, nil); err != nil {
		return fmt.Errorf("failed to purge caches: %w", err)
	}
	return nil
}

// DeleteCache removes a single execution cache entry.
func (c *Client) DeleteCache(ctx context.Context, moduleID, cacheID string) error {
	path := modulePath(moduleID, "/caches/"+url.PathEscape(cacheID))
	if err := c.http().Do(ctx, "DELETE", path, nil, nil); err != nil {
		return fmt.Errorf("failed to delete cache: %w", err)
	}
	return nil
}

// --- Signing keys (write) --------------------------------------------------

// CreateSigningKeyRequest is the `CreateSigningKeyDto` payload.
type CreateSigningKeyRequest struct {
	Algorithm string `json:"algorithm,omitempty"`
	ProductID string `json:"productId,omitempty"`
	PublicKey string `json:"publicKey,omitempty"`
}

// CreateSigningKey issues a new signing key.
func (c *Client) CreateSigningKey(ctx context.Context, body CreateSigningKeyRequest) (*SigningKey, error) {
	var out SigningKey
	if err := c.http().Do(ctx, "POST", platformBase+"/signing-keys", body, &out); err != nil {
		return nil, fmt.Errorf("failed to create signing key: %w", err)
	}
	return &out, nil
}

// RotateSigningKey marks the current key as rotated and issues a new one.
func (c *Client) RotateSigningKey(ctx context.Context, keyID string) (*SigningKey, error) {
	path := platformBase + "/signing-keys/" + url.PathEscape(keyID) + "/rotate"
	var out SigningKey
	if err := c.http().Do(ctx, "POST", path, nil, &out); err != nil {
		return nil, fmt.Errorf("failed to rotate signing key: %w", err)
	}
	return &out, nil
}

// DeleteSigningKey revokes a signing key.
func (c *Client) DeleteSigningKey(ctx context.Context, keyID string) error {
	path := platformBase + "/signing-keys/" + url.PathEscape(keyID)
	if err := c.http().Do(ctx, "DELETE", path, nil, nil); err != nil {
		return fmt.Errorf("failed to delete signing key: %w", err)
	}
	return nil
}

// --- Accreditations (write) ------------------------------------------------

// CreateAccreditationRequest is the `CreateAccreditationDto` payload.
type CreateAccreditationRequest struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Status    string `json:"status,omitempty"`
	Reference string `json:"reference,omitempty"`
	ExpiresAt string `json:"expiresAt,omitempty"`
}

// CreateAccreditation registers an account-level accreditation.
func (c *Client) CreateAccreditation(ctx context.Context, body CreateAccreditationRequest) (*Accreditation, error) {
	var out Accreditation
	if err := c.http().Do(ctx, "POST", platformBase+"/accreditations", body, &out); err != nil {
		return nil, fmt.Errorf("failed to create accreditation: %w", err)
	}
	return &out, nil
}

// DeleteAccreditation removes an accreditation.
func (c *Client) DeleteAccreditation(ctx context.Context, accreditationID string) error {
	path := platformBase + "/accreditations/" + url.PathEscape(accreditationID)
	if err := c.http().Do(ctx, "DELETE", path, nil, nil); err != nil {
		return fmt.Errorf("failed to delete accreditation: %w", err)
	}
	return nil
}

// --- Environment variables (write) -----------------------------------------

// SaveEnvironmentVariableRequest is the create/update payload for an
// environment variable. `ID` is ignored on create and required on update.
type SaveEnvironmentVariableRequest struct {
	ID           string   `json:"-"`
	Key          string   `json:"key"`
	Value        string   `json:"value"`
	Environments []string `json:"environments,omitempty"`
	Visibility   string   `json:"visibility,omitempty"`
	ProductID    string   `json:"productId,omitempty"`
}

// CreateEnvironmentVariable declares a new environment variable.
func (c *Client) CreateEnvironmentVariable(ctx context.Context, body SaveEnvironmentVariableRequest) (*EnvironmentVariable, error) {
	var out EnvironmentVariable
	if err := c.http().Do(ctx, "POST", platformBase+"/environment-variables", body, &out); err != nil {
		return nil, fmt.Errorf("failed to create environment variable: %w", err)
	}
	return &out, nil
}

// UpdateEnvironmentVariable updates an existing environment variable.
func (c *Client) UpdateEnvironmentVariable(ctx context.Context, id string, body SaveEnvironmentVariableRequest) (*EnvironmentVariable, error) {
	path := platformBase + "/environment-variables/" + url.PathEscape(id)
	var out EnvironmentVariable
	if err := c.http().Do(ctx, "PUT", path, body, &out); err != nil {
		return nil, fmt.Errorf("failed to update environment variable: %w", err)
	}
	return &out, nil
}

// DeleteEnvironmentVariable removes an environment variable.
func (c *Client) DeleteEnvironmentVariable(ctx context.Context, id string) error {
	path := platformBase + "/environment-variables/" + url.PathEscape(id)
	if err := c.http().Do(ctx, "DELETE", path, nil, nil); err != nil {
		return fmt.Errorf("failed to delete environment variable: %w", err)
	}
	return nil
}
