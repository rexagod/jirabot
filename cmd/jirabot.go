package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/andygrunwald/go-jira"
	"github.com/rexagod/jirabot/internal"
	"github.com/rexagod/jirabot/pkg"
)

func main() {
	// Instantiate a logger instance.
	logger := log.New(os.Stdout, "jirabot: ", log.Lshortfile)

	// Initialize flags.
	timeoutFlag := flag.String("timeout", "5m", "Timeout duration (e.g., 300ms, 1.5h, 2h45m). Default is 10m.")

	// Parse flags.
	flag.Parse()
	timeout, err := time.ParseDuration(*timeoutFlag)
	if err != nil {
		logger.Printf("Failed to parse timeout duration: %s", err)
		os.Exit(1)
	}

	// Define timeout.
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Instantiate clients.
	githubClient := &http.Client{
		Transport: &pkg.GHTransporter{
			Transport: http.DefaultTransport,
			Token:     os.Getenv("GH_KEY" /* GITHUB_ prefixes are not allowed for repository secrets. */),
		},
	}
	jiraClient, err := jira.NewClient(&http.Client{
		Transport: &pkg.JIRATransporter{
			Transport: http.DefaultTransport,
			Token:     os.Getenv("JIRA_KEY"),
		},
	}, internal.RH_JIRA_INSTANCE_URL)
	if err != nil {
		logger.Printf("Failed to create JIRA client: %s", err)
		os.Exit(1)
	}

	// Verify that the clients work.
	githubRequest, err := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/zen", nil)
	if err != nil {
		logger.Printf("Failed to create GitHub request: %s", err)
		os.Exit(1)
	}
	githubResponse, err := githubClient.Do(githubRequest)
	if err != nil {
		logger.Printf("Encountered unexpected error for GitHub client: %s", err)
		os.Exit(1)
	}
	if githubResponse.StatusCode != http.StatusOK {
		logger.Printf("Received unexpected status code for GitHub client: %s", githubResponse.Status)
		os.Exit(1)
	}
	err = githubResponse.Body.Close()
	if err != nil {
		logger.Printf("Failed to close GitHub response body: %s", err)
		os.Exit(1)
	}
	_, jiraResponse, err := jiraClient.Status.GetAllStatusesWithContext(ctx)
	if err != nil {
		logger.Printf("Encountered unexpected error for JIRA client: %s", err)
		os.Exit(1)
	}
	if jiraResponse.StatusCode != http.StatusOK {
		logger.Printf("Received unexpected status code for JIRA client: %s", jiraResponse.Status)
		os.Exit(1)
	}
	err = jiraResponse.Body.Close()
	if err != nil {
		logger.Printf("Failed to close JIRA response body: %s", err)
		os.Exit(1)
	}

	// Run.
	internal.NewRunner(ctx, internal.NewClient(jiraClient, githubClient, logger)).Run()
}
