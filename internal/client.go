package internal

import (
	"log"
	"net/http"

	"github.com/andygrunwald/go-jira"
)

// Client knows how to perform basic operations.
type Client struct {
	jiraClient   *jira.Client
	githubClient *http.Client
	logger       *log.Logger
}

// NewClient creates a new client.
func NewClient(jiraClient *jira.Client, githubClient *http.Client, logger *log.Logger) *Client {
	return &Client{
		jiraClient:   jiraClient,
		githubClient: githubClient,
		logger:       logger,
	}
}
