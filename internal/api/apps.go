package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func ValidID(id string) bool { return uuidPattern.MatchString(id) }

type App struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Subdomain  string `json:"subdomain"`
	RepoURL    string `json:"repoUrl"`
	Framework  string `json:"framework"`
	RootDir    string `json:"rootDir"`
	AutoDeploy bool   `json:"autoDeploy"`
	CreatedAt  string `json:"createdAt"`
}

type AppDetail struct {
	App App `json:"app"`
}

func (c *Client) App(ctx context.Context, token, id string) (AppDetail, error) {
	var result AppDetail
	if !ValidID(id) {
		return result, errors.New("app ID must be a UUID")
	}
	err := c.get(ctx, "/apps/"+id, token, &result)
	if err == nil && result.App.ID != id {
		err = errors.New("API returned a different app than requested")
	}
	return result, err
}

type CreateApp struct {
	Name      string `json:"name"`
	RepoURL   string `json:"repoUrl"`
	Framework string `json:"framework,omitempty"`
	RootDir   string `json:"rootDir,omitempty"`
}

type DeploymentAccepted struct {
	AppID        string `json:"appId"`
	DeploymentID string `json:"deploymentId"`
	Name         string `json:"name"`
	Subdomain    string `json:"subdomain"`
	Status       string `json:"status"`
}

func (c *Client) CreateApp(ctx context.Context, token string, request CreateApp) (DeploymentAccepted, error) {
	var result DeploymentAccepted
	err := c.post(ctx, "/deploy", token, request, &result)
	if err == nil && (!ValidID(result.AppID) || !ValidID(result.DeploymentID)) {
		err = errors.New("creation response did not include app and deployment UUIDs; check the dashboard before retrying")
	}
	if err != nil {
		return result, mutationError(err)
	}
	return result, nil
}

func mutationError(err error) error {
	var remote *Error
	if errors.As(err, &remote) && remote.Status >= 400 && remote.Status < 500 {
		return err
	}
	return &MutationError{Cause: err}
}

type MutationError struct{ Cause error }

func (e *MutationError) Error() string {
	return "the operation outcome is unknown; check the dashboard before retrying: " + e.Cause.Error()
}
func (e *MutationError) Unwrap() error { return e.Cause }

type Accepted struct {
	AppID  string `json:"appId"`
	JobID  int64  `json:"jobId,omitempty"`
	Status string `json:"status"`
}

type Cancelled struct {
	OK             bool  `json:"ok"`
	CancelledJobID int64 `json:"cancelledJobId"`
}

func (c *Client) CancelDeployment(ctx context.Context, token, id string) (Cancelled, error) {
	var result Cancelled
	if !ValidID(id) {
		return result, errors.New("app ID must be a UUID")
	}
	err := c.post(ctx, "/apps/"+id+"/cancel", token, struct{}{}, &result)
	if err == nil && !result.OK {
		err = errors.New("API did not confirm deployment cancellation")
	}
	if err != nil {
		return result, mutationError(err)
	}
	return result, nil
}

func (c *Client) AppAction(ctx context.Context, token, id, action string) (Accepted, error) {
	var result Accepted
	if !ValidID(id) {
		return result, errors.New("app ID must be a UUID")
	}
	var err error
	switch action {
	case "start", "stop":
		err = c.post(ctx, "/apps/"+id+"/"+action, token, struct{}{}, &result)
		if err == nil && (result.AppID != id || result.Status == "") {
			err = errors.New("API returned an incomplete app operation result")
		}
	case "delete":
		err = c.request(ctx, http.MethodDelete, c.origin+"/user/api/v1/apps/"+id, token, "", nil, nil)
		result.AppID, result.Status = id, "deleted"
	default:
		return result, errors.New("unknown app operation")
	}
	if err != nil {
		return result, mutationError(err)
	}
	return result, nil
}

type Deployment struct {
	ID           string `json:"id"`
	AppID        string `json:"appId,omitempty"`
	Status       string `json:"status"`
	CommitSHA    string `json:"commitSha,omitempty"`
	CreatedAt    string `json:"createdAt"`
	CompletedAt  string `json:"completedAt,omitempty"`
	BuildLogs    string `json:"buildLogs,omitempty"`
	ErrorMessage string `json:"errorMessage,omitempty"`
}

func (d *Deployment) redact(token string) {
	if token != "" {
		d.BuildLogs = strings.ReplaceAll(d.BuildLogs, token, "[REDACTED]")
		d.ErrorMessage = strings.ReplaceAll(d.ErrorMessage, token, "[REDACTED]")
	}
}

type Pagination struct {
	Total   int  `json:"total"`
	Limit   int  `json:"limit"`
	Offset  int  `json:"offset"`
	HasMore bool `json:"hasMore"`
}
type Deployments struct {
	Data       []Deployment `json:"data"`
	Pagination Pagination   `json:"pagination"`
}

func (c *Client) Deployments(ctx context.Context, token, id string, limit, offset int) (Deployments, error) {
	var result Deployments
	if !ValidID(id) {
		return result, errors.New("app ID must be a UUID")
	}
	err := c.get(ctx, "/apps/"+id+"/deployments?limit="+strconv.Itoa(limit)+"&offset="+strconv.Itoa(offset), token, &result)
	if result.Data == nil {
		result.Data = []Deployment{}
	}
	for i := range result.Data {
		result.Data[i].redact(token)
	}
	return result, err
}

func (c *Client) DeploymentLogs(ctx context.Context, token, id string) (Deployment, error) {
	var result Deployment
	if !ValidID(id) {
		return result, errors.New("deployment ID must be a UUID")
	}
	err := c.get(ctx, "/deployments/"+id+"/logs", token, &result)
	result.redact(token)
	if err == nil && result.ID != id {
		err = errors.New("API returned a different deployment than requested")
	}
	return result, err
}

type LogLine struct {
	ID      int64  `json:"id"`
	Seq     int64  `json:"seq"`
	Stream  string `json:"stream"`
	Message string `json:"msg"`
	At      string `json:"at"`
}
type RuntimeLogs struct {
	Lines     []LogLine `json:"lines"`
	NextSince string    `json:"nextSince"`
}

func (c *Client) RuntimeLogs(ctx context.Context, token, id, since string, limit int) (RuntimeLogs, error) {
	var result RuntimeLogs
	if !ValidID(id) {
		return result, errors.New("app ID must be a UUID")
	}
	query := url.Values{"limit": {strconv.Itoa(limit)}}
	if since != "" {
		query.Set("since", since)
	}
	err := c.get(ctx, "/apps/"+id+"/logs/runtime?"+query.Encode(), token, &result)
	if result.Lines == nil {
		result.Lines = []LogLine{}
	}
	if token != "" {
		for i := range result.Lines {
			result.Lines[i].Message = strings.ReplaceAll(result.Lines[i].Message, token, "[REDACTED]")
		}
	}
	return result, err
}
