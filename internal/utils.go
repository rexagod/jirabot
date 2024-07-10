package internal

import (
	"fmt"
	"os"
)

const RH_JIRA_INSTANCE_URL = "https://issues.redhat.com"

func toIssueURL(key string) string {
	return fmt.Sprintf(RH_JIRA_INSTANCE_URL+"/browse/%s", key)
}

func isCI() bool {
	return os.Getenv("CI") == "true"
}
