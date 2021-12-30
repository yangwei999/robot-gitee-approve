package main

import (
	"net/url"
	"regexp"
	"strings"

	sdk "github.com/opensourceways/go-gitee/gitee"
	"github.com/opensourceways/repo-owners-cache/repoowners"
	"github.com/sirupsen/logrus"
	"k8s.io/test-infra/prow/github"

	"github.com/opensourceways/robot-gitee-approve/approve"
	"github.com/opensourceways/robot-gitee-approve/approve/config"
)

const (
	approveCommand = "APPROVE"
	lgtmCommand    = "LGTM"
)

var commandReg = regexp.MustCompile(`(?m)^/([^\s]+)[\t ]*([^\n\r]*)`)

func (bot *robot) loadRepoOwners(org, repo, base string) (repoowners.RepoOwner, error) {
	return repoowners.NewRepoOwners(
		repoowners.RepoBranch{
			Platform: "gitee",
			Org:      org,
			Repo:     repo,
			Branch:   base,
		},
		bot.cacheCli,
	)
}

func (bot *robot) handle(org, repo string, pr *sdk.PullRequestHook, cfg *botConfig, log *logrus.Entry) error {
	targetBranch := pr.GetBase().GetRef()
	oc, err := bot.loadRepoOwners(org, repo, targetBranch)
	if err != nil {
		return err
	}

	var assignees []github.User

	as := pr.GetAssignees()
	if n := len(as); n > 0 {
		assignees := make([]github.User, n)
		for i := range as {
			assignees[i] = github.User{Login: as[i].GetLogin()}
		}
	}

	state := approve.NewState(
		org, repo,
		targetBranch,
		pr.GetBody(),
		pr.GetUser().GetLogin(),
		pr.GetHtmlURL(),
		int(pr.GetNumber()),
		assignees,
	)

	c := transformConfig(org, cfg)

	return approve.Handle(
		log, &bot.cli, oc,
		getGiteeOption(), &c, state,
	)
}

func isApproveCommand(comment string, lgtmActsAsApprove bool) bool {
	for _, match := range commandReg.FindAllStringSubmatch(comment, -1) {
		cmd := strings.ToUpper(match[1])

		if cmd == approveCommand || (cmd == lgtmCommand && lgtmActsAsApprove) {
			return true
		}
	}

	return false
}

func getGiteeOption() config.GitHubOptions {
	s := "https://gitee.com"
	linkURL, _ := url.Parse(s)

	return config.GitHubOptions{LinkURLFromConfig: s, LinkURL: linkURL}
}
