package repository

import (
	"regexp"
	"strings"
)

var (
	githubSSHURL    = regexp.MustCompile(`^git@github\.com:([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+?)(?:\.git)?$`)
	githubSSHScheme = regexp.MustCompile(`^ssh://git@github\.com/([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+?)(?:\.git)?$`)
	githubHTTPSURL  = regexp.MustCompile(`^https://github\.com/([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+?)(?:\.git)?/?$`)
)

type githubRemote struct {
	Owner    string
	Name     string
	SSHURL   string
	HTTPSURL string
}

func parseGitHubRemote(value string) (githubRemote, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return githubRemote{}, invalid("repository URL is required")
	}
	for _, pattern := range []*regexp.Regexp{githubSSHURL, githubSSHScheme, githubHTTPSURL} {
		match := pattern.FindStringSubmatch(value)
		if match == nil {
			continue
		}
		owner, name := match[1], match[2]
		return githubRemote{
			Owner:    owner,
			Name:     name,
			SSHURL:   "git@github.com:" + owner + "/" + name + ".git",
			HTTPSURL: "https://github.com/" + owner + "/" + name + ".git",
		}, nil
	}
	return githubRemote{}, invalid("url must be a GitHub SSH or HTTPS repository in git@github.com:owner/repository.git or https://github.com/owner/repository form")
}

func githubHTTPSURLFromSSH(sshURL string) string {
	remote, err := parseGitHubRemote(sshURL)
	if err != nil {
		return ""
	}
	return remote.HTTPSURL
}

// DeployKeySettingsURL is the GitHub repository Deploy Key page for a
// registered github.com remote. Empty when the URL is not GitHub.
func DeployKeySettingsURL(sshURL string) string {
	remote, err := parseGitHubRemote(sshURL)
	if err != nil {
		return ""
	}
	return "https://github.com/" + remote.Owner + "/" + remote.Name + "/settings/keys"
}
