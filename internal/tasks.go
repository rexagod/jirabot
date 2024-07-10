package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/andygrunwald/go-jira"
)

const (
	JIRA_API_MAX_RESULTS_LIMIT                     = 1000
	DEFAULT_MON_PROJECT_UPSTREAM_ISSUES_JQL_FILTER = "project = MON AND" +
		" resolution = Unresolved AND" +
		" issuetype in (Bug, Task, Sub-task, Story, Epic, Spike) AND" +
		" \"Git Pull Request\" !~ \"https://github.com/openshift\" AND" +
		" \"Git Pull Request\" !~ \"https://gitlab.cee.redhat.com\"" + // Exclude GitLab PRs.
		" ORDER BY priority DESC, updated DESC"

	DEFAULT_MON_PROJECT_INITIAL_STATE      = /* Ticket status when there's a closed or merged PR found. */ "To Do"
	DEFAULT_MON_PROJECT_INTERMEDIATE_STATE = /* Ticket status when there's an open draft-PR found. */ "In Progress"
	DEFAULT_MON_PROJECT_FINAL_STATE        = /* Ticket status when there's an open non-draft-PR found. */ "Code Review"
)

var (
	ProjectUpstreamIssuesJQLFilter = DEFAULT_MON_PROJECT_UPSTREAM_ISSUES_JQL_FILTER

	ProjectInitialState      = DEFAULT_MON_PROJECT_INITIAL_STATE
	ProjectIntermediateState = DEFAULT_MON_PROJECT_INTERMEDIATE_STATE
	ProjectFinalState        = DEFAULT_MON_PROJECT_FINAL_STATE

	mapGitHubToJIRAState = map[string]string{
		"open":   ProjectFinalState,
		"draft":  ProjectIntermediateState,
		"closed": ProjectInitialState,
	}
)

// Runner runs bot-specific tasks under the guise of the provided Client.
type Runner struct {
	ctx         context.Context
	payloadJSON *string
	*Client
}

// NewRunner creates a new runner.
func NewRunner(ctx context.Context, client *Client) *Runner {
	return &Runner{ctx, nil, client}
}

// Run executes the bot.
func (r *Runner) Run() {
	r.respectOverrides()
	if isCI() {
		r.payloadJSON = new(string)
	}
	defer func() {
		if r.payloadJSON == nil || *r.payloadJSON == "" {
			return
		}
		file, err := os.Create("webhook-payload.json")
		if err != nil {
			r.logger.Fatalf("Failed to open file: %s", err)
		}
		defer func(file *os.File) {
			err := file.Close()
			if err != nil {
				r.logger.Fatalf("Failed to close file: %s", err)
			}
		}(file)
		if _, err := file.Write([]byte(fmt.Sprintf("{\"response\": \"%s\"}", *r.payloadJSON))); err != nil {
			r.logger.Fatalf("Failed to write to file: %s", err)
		}
	}()

	// Define tasks.
	tasks := []func(){
		r.verifyIssuesStates,
	}

	var wg sync.WaitGroup
	for _, task := range tasks {
		if r.ctx.Err() != nil {
			r.logger.Printf("[timeout]: %v", r.ctx.Err())

			break
		}
		wg.Add(1)
		go func(task func()) {
			defer wg.Done()
			task()
		}(task)
	}
	wg.Wait()
}

// verifyIssuesStates verifies the states of upstream JIRA issues based on the linked PRs.
func (r *Runner) verifyIssuesStates() {

	// Fetch "all" upstream JIRA issues based on the provided query.
	issues, err := r.getIssues(ProjectUpstreamIssuesJQLFilter)
	if err != nil {
		r.logger.Fatalf("Failed to fetch issues: %s", err)
	}

	// Verify upstream JIRA issue states based on the linked PRs.
	var wg sync.WaitGroup
	for _, issue := range issues {
		wg.Add(1)
		go func(ctx context.Context, issue jira.Issue) {
			defer wg.Done()
			if ctx.Err() != nil {
				r.logger.Printf("[timeout] Skipping %s: %v", issue.Key, ctx.Err())

				return
			}
			if err := r.verifyIssueState(issue); err != nil {
				r.logger.Printf("[%s] Failed to verify issue state: %s", issue.Key, err)
			}
		}(r.ctx, issue)
	}
	wg.Wait()
}

// verifyIssueState verifies the provided issue's state by matching it against the corresponding upstream PR's state.
func (r *Runner) verifyIssueState(issue jira.Issue) error {

	// Check if all provided states are indeed defined for the issue type.
	possibleStates := map[string]struct{}{}
	definedStates := map[string]struct{}{}
	gotPossibleStates, _, err := r.jiraClient.Issue.GetTransitionsWithContext(r.ctx, issue.ID)
	if err != nil {
		return fmt.Errorf("failed to get possible states for issue \"%s\": %w", issue.Key, err)
	}
	for _, possibleState := range gotPossibleStates {
		possibleStates[possibleState.Name] = struct{}{}
	}
	for _, state := range mapGitHubToJIRAState {
		definedStates[state] = struct{}{}
	}
	for state := range definedStates {
		if _, found := possibleStates[state]; !found {
			return fmt.Errorf("state \"%s\" is not a valid state for issue \"%s\"", state, issue.Key)
		}
	}

	// The current state of the issue. This could be any of the states permitted by the project for the issue type.
	jiraMirrorState := issue.Fields.Status.Name

	// Downstream JIRA issues will frequently rely on "links to" to reference the corresponding upstream PRs.
	// TODO: However, this is not the case with upstream issues, since they are not modified by the OpenShift CI Bot
	// TODO: in this manner (although this may be done explicitly, which we'll need to find the custom field for).
	upstreamPRRawListI, found := issue.Fields.Unknowns.Value("customfield_12310220")
	if !found {
		return fmt.Errorf("no linked PRs found for: %s", issue.Key)
	}
	upstreamPRRawList := upstreamPRRawListI.([]interface{})

	// observedJIRAStates helps decide the state of a JIRA issue based on the collective state of all it's linked PRs.
	// For every upstream PR, it records the state that the bot "thinks" should be appropriate for the JIRA issue, and
	// increments its count.
	observedJIRAStates := map[string]int{
		ProjectInitialState:      0,
		ProjectIntermediateState: 0,
		ProjectFinalState:        0,
	}
	for _, upstreamPRRaw := range upstreamPRRawList {
		upstreamPR, found := upstreamPRRaw.(string)
		if !found {
			return fmt.Errorf("invalid linked PR type for: %s", issue.Key)
		}

		// githubMirrorState can be one of the following:
		// * "state" == "closed": The PR is closed or merged.
		// * "state" == "open" && "draft" == false: The PR is up for review.
		// * "state" == "open" && "draft" == true: The PR is open but not up for review.
		githubMirrorState, err := r.getUpstreamPRState(upstreamPR)
		if err != nil {
			return err
		}
		wantJIRAState := mapGitHubToJIRAState[githubMirrorState]
		observedJIRAStates[wantJIRAState] += 1
		if jiraMirrorState != wantJIRAState {
			loggerFlags := r.logger.Flags()
			r.logger.SetFlags(log.Lmsgprefix)
			r.logger.Printf("[DEBUG:PR] [%s] %s [%s] \n\t- [%s] %s\n\t+ [%s] %s", issue.Fields.Summary, upstreamPR, githubMirrorState, jiraMirrorState, toIssueURL(issue.Key), wantJIRAState, toIssueURL(issue.Key))
			r.logger.SetFlags(loggerFlags)
		}
	}

	// Infer the resolved (overall "want") state of the JIRA issue based on the collectively linked PR states.
	resolvedJIRAState := ProjectInitialState
	if observedJIRAStates[ProjectIntermediateState] >= 1 {
		resolvedJIRAState = ProjectIntermediateState
	}
	if observedJIRAStates[ProjectFinalState] >= 1 {
		resolvedJIRAState = ProjectFinalState
	}
	if jiraMirrorState != resolvedJIRAState {
		loggerFlags := r.logger.Flags()
		r.logger.SetFlags(log.Lmsgprefix)
		if r.payloadJSON != nil {
			assigneeInformation := "NO ASSIGNEE"
			if issue.Fields.Assignee != nil {
				assigneeInformation = fmt.Sprintf("[%s](mailto:%s)", issue.Fields.Assignee.DisplayName, issue.Fields.Assignee.EmailAddress)
			}
			*r.payloadJSON += fmt.Sprintf("* [%s](%s), assigned to %s: Expected issue state to be '%s', but got '%s'.\\n", issue.Fields.Summary, toIssueURL(issue.Key), assigneeInformation, resolvedJIRAState, jiraMirrorState)
			r.logger.Printf("[DEBUG:ISSUE] [%s] %s [%s] \n\t- [%s] %s\n\t+ [%s] %s", issue.Fields.Summary, issue.Key, jiraMirrorState, jiraMirrorState, toIssueURL(issue.Key), resolvedJIRAState, toIssueURL(issue.Key))
		}
		r.logger.SetFlags(loggerFlags)
	}

	return nil
}

// getUpstreamPRState gets the state of the provided upstream PR in a manner that is mappable to its JIRA counterpart.
func (r *Runner) getUpstreamPRState(upstreamPR string) (string, error) {

	// Check if the provided URL is a valid GitHub upstream URL.
	githubPrefixForwardSlash := "https://github.com/"
	isValidUpstreamURL := regexp.MustCompile(`^https://github\.com/[^/]+/[^/]+/pull/\d+$`).MatchString(upstreamPR) &&
		!strings.HasPrefix(upstreamPR, githubPrefixForwardSlash+"openshift")
	if !isValidUpstreamURL {
		return "", fmt.Errorf("invalid upstream PR URL: %s", upstreamPR)
	}

	// Fetch the state of the PR.
	upstreamURLInAPICompliantFormat := fmt.Sprintf("https://api.github.com/repos/%s", strings.TrimPrefix(upstreamPR, githubPrefixForwardSlash))
	lastForwardSlashIndex := strings.LastIndex(upstreamURLInAPICompliantFormat, "/")
	upstreamURLInAPICompliantFormat = upstreamURLInAPICompliantFormat[:lastForwardSlashIndex] + "s" + upstreamURLInAPICompliantFormat[lastForwardSlashIndex:]
	request, err := http.NewRequestWithContext(r.ctx, "GET", upstreamURLInAPICompliantFormat, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create upstream PR request: %w", err)
	}
	response, err := r.githubClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("failed to fetch upstream PR state: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read upstream PR response body: %w", err)
	}
	var structuredBody interface{}
	if err := json.Unmarshal(body, &structuredBody); err != nil {
		return "", fmt.Errorf("failed to unmarshal upstream PR response body: %w", err)
	}
	upstreamPRState, found := structuredBody.(map[string]interface{})["state"].(string)
	if !found {
		return "", fmt.Errorf("\"state\" field not found in GH API response: %s", upstreamPR)
	}
	upstreamPRDraft, found := structuredBody.(map[string]interface{})["draft"].(bool)
	if !found {

		return "", fmt.Errorf("\"draft\" field not found in GH API response: %s", upstreamPR)
	}

	// Infer the state of the PR in a manner that is mappable to the JIRA mirror status.
	if upstreamPRState == "open" {
		if !upstreamPRDraft {
			return "open", nil
		}

		return "draft", nil
	} else if upstreamPRState == "closed" {
		return "closed", nil
	}

	return "", fmt.Errorf("unknown upstream PR state: %s", upstreamPRState)
}

// getIssues fetches issues from JIRA, while working around the per-request rate limit.
func (r *Runner) getIssues(jql string) ([]jira.Issue, error) {
	var issues []jira.Issue
	fetchedIssuesCount := 0
	for {
		gotIssues, response, err := r.jiraClient.Issue.SearchWithContext(r.ctx, jql, &jira.SearchOptions{
			StartAt:    fetchedIssuesCount,
			MaxResults: JIRA_API_MAX_RESULTS_LIMIT,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to fetch issues: %w", err)
		}
		issues = append(issues, gotIssues...)
		fetchedIssuesCount += len(gotIssues)
		if fetchedIssuesCount >= response.Total {
			break
		}
	}

	return issues, nil
}

// respectOverrides respects the environment variable overrides.
func (r *Runner) respectOverrides() {
	if value, found := os.LookupEnv("PROJECT_UPSTREAM_ISSUES_JQL_FILTER"); found {
		r.logger.Printf("Overriding PROJECT_UPSTREAM_ISSUES_JQL_FILTER with: %s", value)
		ProjectUpstreamIssuesJQLFilter = value
	}
	if value, found := os.LookupEnv("PROJECT_INITIAL_STATE"); found {
		r.logger.Printf("Overriding PROJECT_INITIAL_STATE with: %s", value)
		ProjectInitialState = value
	}
	if value, found := os.LookupEnv("PROJECT_INTERMEDIATE_STATE"); found {
		r.logger.Printf("Overriding PROJECT_INTERMEDIATE_STATE with: %s", value)
		ProjectIntermediateState = value
	}
	if value, found := os.LookupEnv("PROJECT_FINAL_STATE"); found {
		r.logger.Printf("Overriding PROJECT_FINAL_STATE with: %s", value)
		ProjectFinalState = value
	}
}
