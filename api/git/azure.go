package git

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-git/v5/plumbing/transport/client"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/pkg/errors"
	"github.com/portainer/portainer/api/archive"
)

const (
	azureDevOpsHost        = "dev.azure.com"
	visualStudioHostSuffix = ".visualstudio.com"
)

func isAzureUrl(s string) bool {
	return strings.Contains(s, azureDevOpsHost) ||
		strings.Contains(s, visualStudioHostSuffix)
}

type azureOptions struct {
	organisation, project, repository string
	// a user may pass credentials in a repository URL,
	// for example https://<username>:<password>@<domain>/<path>
	username, password string
}

// azureRef abstracts from the response of https://docs.microsoft.com/en-us/rest/api/azure/devops/git/refs/list?view=azure-devops-rest-6.0#refs
type azureRef struct {
	Name     string `json:"name"`
	ObjectID string `json:"objectId"`
}

// azureItem abstracts from the response of https://docs.microsoft.com/en-us/rest/api/azure/devops/git/items/get?view=azure-devops-rest-6.0#download
type azureItem struct {
	ObjectID string `json:"objectId"`
	CommitId string `json:"commitId"`
	Path     string `json:"path"`
}

type azureDownloader struct {
	client       *http.Client
	baseUrl      string
	cacheEnabled bool
	mu           sync.Mutex
	// Cache the result of repository refs, key is repository URL
	repoRefCache map[string][]string
	// Cache the result of repository file tree, key is the concatenated string of repository URL and ref value
	repoTreeCache map[string][]string
}

func NewAzureDownloader(enableCache bool) *azureDownloader {
	httpsCli := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			Proxy:           http.ProxyFromEnvironment,
		},
		Timeout: 300 * time.Second,
	}

	client.InstallProtocol("https", githttp.NewClient(httpsCli))

	return &azureDownloader{
		client:        httpsCli,
		baseUrl:       "https://dev.azure.com",
		cacheEnabled:  enableCache,
		repoRefCache:  make(map[string][]string),
		repoTreeCache: make(map[string][]string),
	}
}

func (a *azureDownloader) download(ctx context.Context, destination string, options cloneOptions) error {
	zipFilepath, err := a.downloadZipFromAzureDevOps(ctx, options)
	if err != nil {
		return errors.Wrap(err, "failed to download a zip file from Azure DevOps")
	}
	defer os.Remove(zipFilepath)

	err = archive.UnzipFile(zipFilepath, destination)
	if err != nil {
		return errors.Wrap(err, "failed to unzip file")
	}

	return nil
}

func (a *azureDownloader) downloadZipFromAzureDevOps(ctx context.Context, options cloneOptions) (string, error) {
	config, err := parseUrl(options.repositoryUrl)
	if err != nil {
		return "", errors.WithMessage(err, "failed to parse url")
	}
	downloadUrl, err := a.buildDownloadUrl(config, options.referenceName)
	if err != nil {
		return "", errors.WithMessage(err, "failed to build download url")
	}
	zipFile, err := ioutil.TempFile("", "azure-git-repo-*.zip")
	if err != nil {
		return "", errors.WithMessage(err, "failed to create temp file")
	}
	defer zipFile.Close()

	req, err := http.NewRequestWithContext(ctx, "GET", downloadUrl, nil)
	if options.username != "" || options.password != "" {
		req.SetBasicAuth(options.username, options.password)
	} else if config.username != "" || config.password != "" {
		req.SetBasicAuth(config.username, config.password)
	}

	if err != nil {
		return "", errors.WithMessage(err, "failed to create a new HTTP request")
	}

	res, err := a.client.Do(req)
	if err != nil {
		return "", errors.WithMessage(err, "failed to make an HTTP request")
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to download zip with a status \"%v\"", res.Status)
	}

	_, err = io.Copy(zipFile, res.Body)
	if err != nil {
		return "", errors.WithMessage(err, "failed to save HTTP response to a file")
	}
	return zipFile.Name(), nil
}

func (a *azureDownloader) latestCommitID(ctx context.Context, options fetchOptions) (string, error) {
	rootItem, err := a.getRootItem(ctx, options)
	if err != nil {
		return "", err
	}
	return rootItem.CommitId, nil
}

func (a *azureDownloader) getRootItem(ctx context.Context, options fetchOptions) (*azureItem, error) {
	config, err := parseUrl(options.repositoryUrl)
	if err != nil {
		return nil, errors.WithMessage(err, "failed to parse url")
	}

	rootItemUrl, err := a.buildRootItemUrl(config, options.referenceName)
	if err != nil {
		return nil, errors.WithMessage(err, "failed to build azure root item url")
	}

	req, err := http.NewRequestWithContext(ctx, "GET", rootItemUrl, nil)
	if options.username != "" || options.password != "" {
		req.SetBasicAuth(options.username, options.password)
	} else if config.username != "" || config.password != "" {
		req.SetBasicAuth(config.username, config.password)
	}

	if err != nil {
		return nil, errors.WithMessage(err, "failed to create a new HTTP request")
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, errors.WithMessage(err, "failed to make an HTTP request")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get repository root item with a status \"%v\"", resp.Status)
	}

	var items struct {
		Value []azureItem
	}

	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, errors.Wrap(err, "could not parse Azure items response")
	}

	if len(items.Value) == 0 || items.Value[0].CommitId == "" {
		return nil, errors.Errorf("failed to get latest commitID in the repository")
	}
	return &items.Value[0], nil
}

func parseUrl(rawUrl string) (*azureOptions, error) {
	if strings.HasPrefix(rawUrl, "https://") || strings.HasPrefix(rawUrl, "http://") {
		return parseHttpUrl(rawUrl)
	}
	if strings.HasPrefix(rawUrl, "git@ssh") {
		return parseSshUrl(rawUrl)
	}
	if strings.HasPrefix(rawUrl, "ssh://") {
		r := []rune(rawUrl)
		return parseSshUrl(string(r[6:])) // remove the prefix
	}

	return nil, errors.Errorf("supported url schemes are https and ssh; recevied URL %s rawUrl", rawUrl)
}

var expectedSshUrl = "git@ssh.dev.azure.com:v3/Organisation/Project/Repository"

func parseSshUrl(rawUrl string) (*azureOptions, error) {
	path := strings.Split(rawUrl, "/")

	unexpectedUrlErr := errors.Errorf("want url %s, got %s", expectedSshUrl, rawUrl)
	if len(path) != 4 {
		return nil, unexpectedUrlErr
	}
	return &azureOptions{
		organisation: path[1],
		project:      path[2],
		repository:   path[3],
	}, nil
}

const expectedAzureDevOpsHttpUrl = "https://Organisation@dev.azure.com/Organisation/Project/_git/Repository"
const expectedVisualStudioHttpUrl = "https://organisation.visualstudio.com/project/_git/repository"

func parseHttpUrl(rawUrl string) (*azureOptions, error) {
	u, err := url.Parse(rawUrl)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse HTTP url")
	}

	opt := azureOptions{}
	switch {
	case u.Host == azureDevOpsHost:
		path := strings.Split(u.Path, "/")
		if len(path) != 5 {
			return nil, errors.Errorf("want url %s, got %s", expectedAzureDevOpsHttpUrl, u)
		}
		opt.organisation = path[1]
		opt.project = path[2]
		opt.repository = path[4]
	case strings.HasSuffix(u.Host, visualStudioHostSuffix):
		path := strings.Split(u.Path, "/")
		if len(path) != 4 {
			return nil, errors.Errorf("want url %s, got %s", expectedVisualStudioHttpUrl, u)
		}
		opt.organisation = strings.TrimSuffix(u.Host, visualStudioHostSuffix)
		opt.project = path[1]
		opt.repository = path[3]
	default:
		return nil, errors.Errorf("unknown azure host in url \"%s\"", rawUrl)
	}

	opt.username = u.User.Username()
	opt.password, _ = u.User.Password()

	return &opt, nil
}

func (a *azureDownloader) buildDownloadUrl(config *azureOptions, referenceName string) (string, error) {
	rawUrl := fmt.Sprintf("%s/%s/%s/_apis/git/repositories/%s/items",
		a.baseUrl,
		url.PathEscape(config.organisation),
		url.PathEscape(config.project),
		url.PathEscape(config.repository))
	u, err := url.Parse(rawUrl)

	if err != nil {
		return "", errors.Wrapf(err, "failed to parse download url path %s", rawUrl)
	}
	q := u.Query()
	// scopePath=/&download=true&versionDescriptor.version=main&$format=zip&recursionLevel=full&api-version=6.0
	q.Set("scopePath", "/")
	q.Set("download", "true")
	if referenceName != "" {
		q.Set("versionDescriptor.versionType", getVersionType(referenceName))
		q.Set("versionDescriptor.version", formatReferenceName(referenceName))
	}
	q.Set("$format", "zip")
	q.Set("recursionLevel", "full")
	q.Set("api-version", "6.0")
	u.RawQuery = q.Encode()

	return u.String(), nil
}

func (a *azureDownloader) buildRootItemUrl(config *azureOptions, referenceName string) (string, error) {
	rawUrl := fmt.Sprintf("%s/%s/%s/_apis/git/repositories/%s/items",
		a.baseUrl,
		url.PathEscape(config.organisation),
		url.PathEscape(config.project),
		url.PathEscape(config.repository))
	u, err := url.Parse(rawUrl)

	if err != nil {
		return "", errors.Wrapf(err, "failed to parse root item url path %s", rawUrl)
	}

	q := u.Query()
	q.Set("scopePath", "/")
	if referenceName != "" {
		q.Set("versionDescriptor.versionType", getVersionType(referenceName))
		q.Set("versionDescriptor.version", formatReferenceName(referenceName))
	}
	q.Set("api-version", "6.0")
	u.RawQuery = q.Encode()

	return u.String(), nil
}

func (a *azureDownloader) buildRefsUrl(config *azureOptions) (string, error) {
	// ref@https://docs.microsoft.com/en-us/rest/api/azure/devops/git/refs/list?view=azure-devops-rest-6.0#gitref
	rawUrl := fmt.Sprintf("%s/%s/%s/_apis/git/repositories/%s/refs",
		a.baseUrl,
		url.PathEscape(config.organisation),
		url.PathEscape(config.project),
		url.PathEscape(config.repository))
	u, err := url.Parse(rawUrl)

	if err != nil {
		return "", errors.Wrapf(err, "failed to parse list refs url path %s", rawUrl)
	}

	q := u.Query()
	q.Set("api-version", "6.0")
	u.RawQuery = q.Encode()

	return u.String(), nil
}

func (a *azureDownloader) buildTreeUrl(config *azureOptions, rootObjectHash string) (string, error) {
	// ref@https://docs.microsoft.com/en-us/rest/api/azure/devops/git/trees/get?view=azure-devops-rest-6.0
	rawUrl := fmt.Sprintf("%s/%s/%s/_apis/git/repositories/%s/trees/%s",
		a.baseUrl,
		url.PathEscape(config.organisation),
		url.PathEscape(config.project),
		url.PathEscape(config.repository),
		url.PathEscape(rootObjectHash),
	)
	u, err := url.Parse(rawUrl)

	if err != nil {
		return "", errors.Wrapf(err, "failed to parse list tree url path %s", rawUrl)
	}
	q := u.Query()
	// projectId={projectId}&recursive=true&fileName={fileName}&$format={$format}&api-version=6.0
	q.Set("recursive", "true")
	q.Set("api-version", "6.0")
	u.RawQuery = q.Encode()

	return u.String(), nil
}

const (
	branchPrefix = "refs/heads/"
	tagPrefix    = "refs/tags/"
)

func formatReferenceName(name string) string {
	if strings.HasPrefix(name, branchPrefix) {
		return strings.TrimPrefix(name, branchPrefix)
	}
	if strings.HasPrefix(name, tagPrefix) {
		return strings.TrimPrefix(name, tagPrefix)
	}
	return name
}

func getVersionType(name string) string {
	if strings.HasPrefix(name, branchPrefix) {
		return "branch"
	}
	if strings.HasPrefix(name, tagPrefix) {
		return "tag"
	}
	return "commit"
}

func (a *azureDownloader) listRemote(ctx context.Context, options cloneOptions) ([]string, error) {
	config, err := parseUrl(options.repositoryUrl)
	if err != nil {
		return nil, errors.WithMessage(err, "failed to parse url")
	}

	listRefsUrl, err := a.buildRefsUrl(config)
	if err != nil {
		return nil, errors.WithMessage(err, "failed to build list refs url")
	}

	req, err := http.NewRequestWithContext(ctx, "GET", listRefsUrl, nil)
	if options.username != "" || options.password != "" {
		req.SetBasicAuth(options.username, options.password)
	} else if config.username != "" || config.password != "" {
		req.SetBasicAuth(config.username, config.password)
	}

	if err != nil {
		return nil, errors.WithMessage(err, "failed to create a new HTTP request")
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, errors.WithMessage(err, "failed to make an HTTP request")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusNotFound {
			return nil, ErrIncorrectRepositoryURL
		} else if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusNonAuthoritativeInfo {
			return nil, ErrAuthenticationFailure
		}
		return nil, fmt.Errorf("failed to list refs url with a status \"%v\"", resp.Status)
	}

	var refs struct {
		Value []azureRef
	}

	if err := json.NewDecoder(resp.Body).Decode(&refs); err != nil {
		return nil, errors.Wrap(err, "could not parse Azure refs response")
	}

	var ret []string
	for _, value := range refs.Value {
		if value.Name == "HEAD" {
			continue
		}
		ret = append(ret, value.Name)
	}

	if a.cacheEnabled {
		a.mu.Lock()
		defer a.mu.Unlock()
		a.repoRefCache[options.repositoryUrl] = ret
	}
	return ret, nil
}

func (a *azureDownloader) listTree(ctx context.Context, options fetchOptions) ([]string, error) {
	var (
		allPaths    []string
		filteredRet []string
		refs        []string
		err         error
	)

	repoKey := generateCacheKey(options.repositoryUrl, options.referenceName)
	treeCache, ok := a.repoTreeCache[repoKey]
	if ok {
		for _, path := range treeCache {
			if matchExtensions(path, options.extensions) {
				filteredRet = append(filteredRet, path)
			}
		}
		return filteredRet, nil
	}

	// Check if the reference exists
	refCache, ok := a.repoRefCache[options.repositoryUrl]
	if ok {
		refs = refCache
	} else {
		opt := cloneOptions{
			repositoryUrl: options.repositoryUrl,
			username:      options.username,
			password:      options.password,
		}
		refs, err = a.listRemote(ctx, opt)
		if err != nil {
			return nil, err
		}
	}
	matchedRef := false
	for _, ref := range refs {
		if ref == options.referenceName {
			matchedRef = true
			break
		}
	}
	if !matchedRef {
		return nil, ErrRefNotFound
	}

	//
	rootItem, err := a.getRootItem(ctx, options)
	if err != nil {
		return nil, err
	}

	config, err := parseUrl(options.repositoryUrl)
	if err != nil {
		return nil, errors.WithMessage(err, "failed to parse url")
	}

	listTreeUrl, err := a.buildTreeUrl(config, rootItem.ObjectID)
	if err != nil {
		return nil, errors.WithMessage(err, "failed to build list tree url")
	}

	req, err := http.NewRequestWithContext(ctx, "GET", listTreeUrl, nil)
	if options.username != "" || options.password != "" {
		req.SetBasicAuth(options.username, options.password)
	} else if config.username != "" || config.password != "" {
		req.SetBasicAuth(config.username, config.password)
	}

	if err != nil {
		return nil, errors.WithMessage(err, "failed to create a new HTTP request")
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, errors.WithMessage(err, "failed to make an HTTP request")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to list tree url with a status \"%v\"", resp.Status)
	}

	var tree struct {
		TreeEntries []struct {
			RelativePath string `json:"relativePath"`
		} `json:"treeEntries"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&tree); err != nil {
		return nil, errors.Wrap(err, "could not parse Azure tree response")
	}

	for _, treeEntry := range tree.TreeEntries {
		allPaths = append(allPaths, treeEntry.RelativePath)
		if matchExtensions(treeEntry.RelativePath, options.extensions) {
			filteredRet = append(filteredRet, treeEntry.RelativePath)
		}
	}

	if a.cacheEnabled {
		a.mu.Lock()
		defer a.mu.Unlock()
		a.repoTreeCache[repoKey] = allPaths
	}

	return filteredRet, nil
}

func (a *azureDownloader) removeCache(ctx context.Context, opt cloneOptions) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.repoRefCache, opt.repositoryUrl)
	repoKey := generateCacheKey(opt.repositoryUrl, opt.referenceName)
	delete(a.repoTreeCache, repoKey)
}
