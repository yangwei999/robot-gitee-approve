package main

import "k8s.io/test-infra/prow/github"

type ghclient struct {
	cli iClient
}

func (c *ghclient) GetPullRequestChanges(org, repo string, number int) ([]github.PullRequestChange, error) {
	cs, err := c.cli.GetPullRequestChanges(org, repo, int32(number))
	if err != nil {
		return nil, err
	}

	return transformPRChanges(cs), nil
}

func (c *ghclient) GetIssueLabels(org, repo string, number int) ([]github.Label, error) {
	labels, err := c.cli.GetPRLabels(org, repo, int32(number))
	if err != nil {
		return nil, err
	}

	return transformLabels(labels), nil
}

func (c *ghclient) ListIssueComments(org, repo string, number int) ([]github.IssueComment, error) {
	comments, err := c.cli.ListPRComments(org, repo, int32(number))
	if err != nil {
		return nil, err
	}

	return transformComments(comments), nil
}

func (c *ghclient) DeleteComment(org, repo string, ID int) error {
	return c.cli.DeletePRComment(org, repo, int32(ID))
}

func (c *ghclient) CreateComment(org, repo string, number int, comment string) error {
	return c.cli.CreatePRComment(org, repo, int32(number), comment)
}

func (c *ghclient) BotName() (string, error) {
	bot, err := c.cli.GetBot()
	if err != nil {
		return "", err
	}
	return bot.Login, nil
}

func (c *ghclient) AddLabel(org, repo string, number int, label string) error {
	return c.cli.AddPRLabel(org, repo, int32(number), label)
}

func (c *ghclient) RemoveLabel(org, repo string, number int, label string) error {
	return c.cli.RemovePRLabel(org, repo, int32(number), label)
}

func (c *ghclient) ListIssueEvents(org, repo string, num int) ([]github.ListedIssueEvent, error) {
	return []github.ListedIssueEvent{}, nil
}

func (c *ghclient) GetPullRequest(org, repo string, number int) (*github.PullRequest, error) {
	return nil, nil
}

func (c *ghclient) ListReviews(org, repo string, number int) ([]github.Review, error) {
	return []github.Review{}, nil
}

func (c *ghclient) ListPullRequestComments(org, repo string, number int) ([]github.ReviewComment, error) {
	return []github.ReviewComment{}, nil
}
