# `jirabot` 🤖

[![CI](https://github.com/rexagod/jirabot/actions/workflows/ci.yaml/badge.svg)](https://github.com/rexagod/jirabot/actions/workflows/ci.yaml) [![Go Report Card](https://goreportcard.com/badge/github.com/rexagod/jirabot)](https://goreportcard.com/report/github.com/rexagod/jirabot) [![Go Reference](https://pkg.go.dev/badge/github.com/rexagod/jirabot.svg)](https://pkg.go.dev/github.com/rexagod/jirabot)

## Summary

`jirabot` builds on top of [`go-jira`](https://pkg.go.dev/github.com/andygrunwald/go-jira#readme-api) to provide a way of automating useful JIRA workflows for use-cases that no official tooling currently targets, or at least not with the same granularity of a turing-complete language (since JIRA ScriptRunner has pre-defined constructs and relies heavily on JQL).

## Philosophy

There's some guidelines that the bot aims to adhere by, these are:
* Don't overlap actions with other team's automation workflows.
* Don't make any changes to issues themselves, only propose them (since rolling back would be very cumbersome).

## Workflow

The bot currently supports the following workflows:

### Verifying states for JIRA issues corresponding to an upstream PR

This workflow consists of the following steps:

> [!IMPORTANT]
>
> #### The desired initial state for a JIRA issue, from the perspective of the bot, is the same for two PRs that are closed and merged, as the bot avoids making "hard-edits", such as closing JIRA issues if the corresponding PR is in a closed or merged state.

* Gather "all" upstream JIRA issues in a project using a JQL query (`PROJECT_UPSTREAM_ISSUES_JQL_FILTER`).
* For each issue,
  * get the corresponding PRs from the "Git Pull Request" field;
  * for each linked PR,
    * validate if it is indeed upstream;
    * record the current state of the PR, and,
    * record the appropriate state for the JIRA issue based on each PR state.
  * Finally, resolve various observed states from different PRs under the JIRA issue to determine the final state, and,
  * match it against the JIRA issue's current state.
* If the PR state does not match the JIRA issue's state, log the proposed diff (instead of transitioning the JIRA issue; commented out for demo purposes at the moment).

## Flags

Currently, the bot supports the following flags:

* `--timeout`: The timeout for the bot to wait for all operations to complete. Defaults to 5 minutes.

## Overrides

Currently, the bot supports the following overrides:

* `PROJECT_UPSTREAM_ISSUES_JQL_FILTER`: The JQL query used to gather "all" upstream issues and for the overall scope for the bot to operate on. Defaults to the monitoring team's `DEFAULT_MON_PROJECT_UPSTREAM_ISSUES_JQL_FILTER`.
* `PROJECT_INITIAL_STATE`: The initial ticket status in a particular project at the time of its creation. Defaults to `To Do`.
* `PROJECT_INTERMEDIATE_STATE`: The intermediate ticket status in a particular project before it is up for review. Defaults to `In Progress`.
* `PROJECT_FINAL_STATE`: The final ticket status in a particular project before it is closed. Defaults to `Code Review`.

###### [License](./LICENSE)
