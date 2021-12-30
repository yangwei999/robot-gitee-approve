package main

import (
	"time"

	sdk "github.com/opensourceways/go-gitee/gitee"
	"k8s.io/test-infra/prow/github"

	"github.com/opensourceways/robot-gitee-approve/approve/plugins"
)

func transformPRChanges(changes []sdk.PullRequestFiles) []github.PullRequestChange {
	n := len(changes)
	if n == 0 {
		return nil
	}

	res := make([]github.PullRequestChange, n)

	for i := range changes {
		v := &changes[i]

		res[i] = github.PullRequestChange{
			SHA:      v.Sha,
			Filename: v.Filename,
			Status:   v.Status,
		}
	}

	return res
}

func transformLabels(labels []sdk.Label) []github.Label {
	n := len(labels)
	if n == 0 {
		return nil
	}

	res := make([]github.Label, n)

	for i := range labels {
		v := &labels[i]

		res[i] = github.Label{
			URL:   v.Url,
			Name:  v.Name,
			Color: v.Color,
		}
	}

	return res
}

func transformComments(comments []sdk.PullRequestComments) []github.IssueComment {
	n := len(comments)
	if n == 0 {
		return nil
	}

	res := make([]github.IssueComment, n)

	parseTime := func(t string) time.Time {
		r, _ := time.Parse(time.RFC3339, t)

		return r
	}

	for i := range comments {
		v := &comments[i]

		res[i] = github.IssueComment{
			ID:        int(v.Id),
			Body:      v.Body,
			User:      transformUser(v.User),
			HTMLURL:   v.HtmlUrl,
			CreatedAt: parseTime(v.CreatedAt),
			UpdatedAt: parseTime(v.UpdatedAt),
		}
	}

	return res
}

func transformUser(user *sdk.UserBasic) github.User {
	return github.User{
		Login:   user.GetLogin(),
		Name:    user.GetName(),
		Email:   user.GetEmail(),
		ID:      int(user.GetID()),
		HTMLURL: user.GetHtmlUrl(),
		Type:    user.GetType(),
	}
}

func transformConfig(org string, cfg *botConfig) plugins.Approve {
	return plugins.Approve{
		Repos:               []string{org},
		RequireSelfApproval: &cfg.RequireSelfApproval,
		IgnoreReviewState:   &cfg.ignoreReviewState,
	}
}
